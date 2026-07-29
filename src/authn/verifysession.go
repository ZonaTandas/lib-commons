// Package authn concentra la verificación de identidad entre servicios:
// la sesión de usuario contra oauth-service (VerifySession) y el service
// token compartido (CheckServiceToken).
//
// Antes de este paquete, los diez servicios con `middleware.Auth` copiaban el
// mismo cliente HTTP a mano (`http.NewRequest` + `http.Client{}` +
// `obs.SetInternalAuth`), con la URL de oauth a veces hardcodeada al dominio
// público. Esa copia no propagaba el traceId, así que el salto a oauth —el que
// más se investiga cuando falla un login— no aparecía en la traza.
//
// VerifySession se construye sobre obs.NewRequest/obs.Do, que ponen traceId y
// X-Internal-Auth solos, y devuelve errores tipados para que cada servicio
// conserve su propio mapeo de códigos HTTP.
package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ZonaTandas/lib-commons/src/obs"
)

// User es la identidad que devuelve oauth-service en /verify/{sessionToken}.
type User struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
}

var (
	// ErrNoSession: la petición no traía token de sesión (o venía vacío).
	// El llamante responde 401, nunca 503: no es un fallo nuestro.
	ErrNoSession = errors.New("authn: no session token")
	// ErrInvalidSession: oauth respondió que la sesión no vale (401/403/404).
	// El llamante responde 401.
	ErrInvalidSession = errors.New("authn: invalid or expired session")
	// ErrUpstream: no se pudo preguntar a oauth (red, timeout, 5xx, respuesta
	// ilegible). El llamante responde 502/503 — es indisponibilidad, no falta
	// de credenciales, y confundirlos manda al usuario a re-loguearse sin
	// motivo.
	ErrUpstream = errors.New("authn: oauth-service unavailable")
	// ErrNotConfigured: falta la URL base de oauth. Fail-closed.
	ErrNotConfigured = errors.New("authn: oauth base url not configured")
)

// DefaultClient es el cliente que usa VerifySession cuando no se le pasa uno.
// Timeout explícito: sin él, un oauth colgado agota los workers del llamante.
var DefaultClient = &http.Client{Timeout: 5 * time.Second}

// BearerToken extrae el token de una cabecera Authorization. Tolera el doble
// prefijo "Bearer Bearer <x>" que llegó a mandar algún BFF, y devuelve ""
// cuando no hay token utilizable.
func BearerToken(authorizationHeader string) string {
	v := strings.TrimSpace(authorizationHeader)
	for {
		trimmed := strings.TrimSpace(strings.TrimPrefix(v, "Bearer "))
		if trimmed == v {
			break
		}
		v = trimmed
	}
	if strings.EqualFold(v, "Bearer") {
		return ""
	}
	return v
}

// VerifySession pregunta a oauth-service por el token de sesión y devuelve el
// usuario verificado.
//
//   - oauthBase: URL base del servicio (p. ej. http://oauth-service.services
//     .svc.cluster.local:8080). Siempre por parámetro: nunca hardcodeada.
//   - sessionToken: el uuid de sesión ya sin el prefijo "Bearer ".
//   - serviceToken: AUTH_SERVICE_TOKEN, que oauth exige en /verify.
//
// La request lleva traceId y X-Internal-Auth automáticamente (obs.NewRequest).
func VerifySession(ctx context.Context, oauthBase, sessionToken, serviceToken string) (User, error) {
	return VerifySessionWithClient(ctx, DefaultClient, oauthBase, sessionToken, serviceToken)
}

// VerifySessionWithClient es VerifySession con un *http.Client propio (tests,
// o un servicio que quiera su propio timeout/transport).
func VerifySessionWithClient(ctx context.Context, client *http.Client, oauthBase, sessionToken, serviceToken string) (User, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return User{}, ErrNoSession
	}
	if strings.TrimSpace(oauthBase) == "" {
		return User{}, ErrNotConfigured
	}

	url := fmt.Sprintf("%s/verify/%s", strings.TrimRight(oauthBase, "/"), sessionToken)
	req, err := obs.NewRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if serviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+serviceToken)
	}

	resp, err := obs.Do(ctx, client, req)
	if err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusOK:
		// sigue abajo
	case resp.StatusCode >= 500:
		return User{}, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	default:
		// 401/403/404 y cualquier otro 4xx: la sesión no vale.
		return User{}, fmt.Errorf("%w: status %d", ErrInvalidSession, resp.StatusCode)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return User{}, fmt.Errorf("%w: respuesta ilegible: %v", ErrUpstream, err)
	}
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	// El contrato de /verify siempre trae id y email. Una respuesta a la que
	// le falte uno no es "usuario sin email": es oauth devolviendo algo roto,
	// y aceptarla propaga un UserID/Email vacío aguas abajo que revienta más
	// tarde y más lejos.
	if user.ID == "" || user.Email == "" {
		return User{}, fmt.Errorf("%w: respuesta incompleta (id=%q email=%q)", ErrUpstream, user.ID, user.Email)
	}
	return user, nil
}
