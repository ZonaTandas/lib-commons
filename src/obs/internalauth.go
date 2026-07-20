package obs

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Verificación de origen interno (evolutivo 2026-07-red-interna-auth).
//
// Toda petición entre servicios lleva la cabecera X-Internal-Auth con uno de
// estos dos valores, ambos derivados de INTERNAL_SHARED_SECRET:
//
//  1. El secreto compartido tal cual (lo que mandan los BFF y obs.NewRequest/Do).
//  2. Un token caducable "v1.<subject>.<expUnix>.<hmac>" firmado con el secreto
//     (NewInternalAuthToken). Pensado para acceso puntual de developers: un
//     servicio con API de emisión (futuro) — o cualquiera con el secreto —
//     puede emitir un token de N minutos sin repartir el secreto, y todos los
//     servicios lo validan offline (HMAC-SHA256, sin llamada extra).
//
// RequireInternalAuth valida la cabecera en la entrada. Dos modos:
//   - INTERNAL_AUTH_ENFORCE != "true" (default): log-only. Deja pasar todo
//     pero loguea internal_auth_missing/invalid, para descubrir llamantes sin
//     migrar antes de activar el enforce (fase 1 del rollout).
//   - INTERNAL_AUTH_ENFORCE = "true": 403 si falta o no valida (fase 2).
//
// Exenciones: /health y /metrics siempre (como obs.Middleware), más los
// prefijos de INTERNAL_AUTH_EXEMPT_PATHS (coma-separados) para rutas que
// reciben tráfico externo legítimo: webhooks (payment-stripe:
// /webhooks/stripe) o contenido público que carga el navegador
// (contentmanager: /contents).
//
// En Grafana: counter internal_auth_requests_total{result,route} (lo registra
// obsmetrics vía SetInternalAuthObserver) + los logs de arriba en Loki.

// internalAuthObserver lo registra obsmetrics (u otro) para contar
// resultados sin meter client_golang en el núcleo stdlib de obs.
var internalAuthObserver func(result, route string)

// SetInternalAuthObserver registra el callback de métricas de
// RequireInternalAuth. result: ok_secret|ok_token|missing|invalid|expired|
// unconfigured|exempt.
func SetInternalAuthObserver(f func(result, route string)) { internalAuthObserver = f }

// InternalAuthSecret devuelve el secreto compartido ("" si no configurado).
func InternalAuthSecret() string { return os.Getenv("INTERNAL_SHARED_SECRET") }

// SetInternalAuth añade X-Internal-Auth a una request saliente si el secreto
// está configurado y la cabecera no venía ya puesta. obs.NewRequest y obs.Do
// lo hacen solos; este helper es para los clientes que construyen la request
// a mano (middleware/auth.go de los servicios, brokers).
func SetInternalAuth(req *http.Request) {
	if req.Header.Get(HeaderInternalAuth) != "" {
		return
	}
	if s := InternalAuthSecret(); s != "" {
		req.Header.Set(HeaderInternalAuth, s)
	}
}

const internalTokenPrefix = "v1"

// NewInternalAuthToken emite un token caducable "v1.<subject>.<exp>.<hmac>"
// firmado con INTERNAL_SHARED_SECRET. subject identifica al portador en logs
// y métricas (p. ej. "dev-alejandro"); solo [A-Za-z0-9_-]. Es la pieza que
// usará la futura API de emisión de tokens de developer.
func NewInternalAuthToken(subject string, ttl time.Duration) (string, error) {
	secret := InternalAuthSecret()
	if secret == "" {
		return "", fmt.Errorf("obs: INTERNAL_SHARED_SECRET no configurado")
	}
	if subject == "" || strings.ContainsAny(subject, ". \t\n") {
		return "", fmt.Errorf("obs: subject de token interno inválido: %q", subject)
	}
	if ttl <= 0 {
		return "", fmt.Errorf("obs: ttl de token interno debe ser > 0")
	}
	exp := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%s.%s.%d", internalTokenPrefix, subject, exp)
	return payload + "." + internalTokenSign(secret, payload), nil
}

func internalTokenSign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateInternalAuth valida el valor de la cabecera contra el secreto:
// o el secreto en claro, o un token caducable bien firmado y no expirado.
// Devuelve el subject ("" si era el secreto) y el resultado
// (ok_secret|ok_token|missing|invalid|expired|unconfigured).
func ValidateInternalAuth(secret, value string) (subject, result string) {
	if secret == "" {
		return "", "unconfigured"
	}
	if value == "" {
		return "", "missing"
	}
	if subtle.ConstantTimeCompare([]byte(value), []byte(secret)) == 1 {
		return "", "ok_secret"
	}
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] != internalTokenPrefix {
		return "", "invalid"
	}
	payload := strings.Join(parts[:3], ".")
	if !hmac.Equal([]byte(internalTokenSign(secret, payload)), []byte(parts[3])) {
		return "", "invalid"
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", "invalid"
	}
	if time.Now().Unix() > exp {
		return "", "expired"
	}
	return parts[1], "ok_token"
}

// RequireInternalAuth es el middleware de verificación de origen interno.
// Instalar DESPUÉS de obs.Middleware (para heredar traceId y bolsa de campos):
//
//	router.Use(obs.Middleware, obs.RequireInternalAuth, obsmetrics.Middleware)
func RequireInternalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := RouteTemplate(r)
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" || internalAuthExempt(r.URL.Path) {
			observeInternalAuth("exempt", route)
			next.ServeHTTP(w, r)
			return
		}

		secret := InternalAuthSecret()
		subject, result := ValidateInternalAuth(secret, r.Header.Get(HeaderInternalAuth))
		observeInternalAuth(result, route)

		switch result {
		case "ok_secret":
			next.ServeHTTP(w, r)
			return
		case "ok_token":
			// El subject sale en la línea de acceso y en Logger(ctx): auditoría
			// de qué developer/herramienta entró con token caducable.
			Add(r.Context(), "internalAuthSubject", subject)
			next.ServeHTTP(w, r)
			return
		}

		enforce := os.Getenv("INTERNAL_AUTH_ENFORCE") == "true"
		msg := "internal_auth_invalid"
		if result == "missing" {
			msg = "internal_auth_missing"
		}
		Logger(r.Context()).LogAttrs(r.Context(), slog.LevelWarn, msg,
			slog.String("route", route),
			slog.String("path", r.URL.Path),
			slog.String("method", r.Method),
			slog.String("reason", result),
			slog.Bool("enforced", enforce),
		)

		// Fase 1 (log-only) o secreto sin configurar en log-only: dejar pasar.
		if !enforce {
			next.ServeHTTP(w, r)
			return
		}
		// Enforce sin secreto configurado = misconfig: se cierra igualmente
		// (fail-closed, como el contrato del panel).
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden: not an internal request"}`))
	})
}

// internalAuthExempt: cada entrada de INTERNAL_AUTH_EXEMPT_PATHS es un
// prefijo de ruta (misma semántica de prefijos que OBS_SAMPLE_PATHS).
func internalAuthExempt(path string) bool {
	v := os.Getenv("INTERNAL_AUTH_EXEMPT_PATHS")
	if v == "" {
		return false
	}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" && strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func observeInternalAuth(result, route string) {
	if internalAuthObserver != nil {
		internalAuthObserver(result, route)
	}
}
