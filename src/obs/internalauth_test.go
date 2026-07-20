package obs

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func doReq(t *testing.T, h http.Handler, path, header string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if header != "" {
		req.Header.Set(HeaderInternalAuth, header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestValidateInternalAuth(t *testing.T) {
	secret := "super-secret"

	if _, r := ValidateInternalAuth("", "lo-que-sea"); r != "unconfigured" {
		t.Fatalf("sin secreto: %s", r)
	}
	if _, r := ValidateInternalAuth(secret, ""); r != "missing" {
		t.Fatalf("sin header: %s", r)
	}
	if _, r := ValidateInternalAuth(secret, secret); r != "ok_secret" {
		t.Fatalf("secreto en claro: %s", r)
	}
	if _, r := ValidateInternalAuth(secret, "otro-valor"); r != "invalid" {
		t.Fatalf("secreto incorrecto: %s", r)
	}
}

func TestInternalAuthToken(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "super-secret")

	tok, err := NewInternalAuthToken("dev-alejandro", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sub, r := ValidateInternalAuth("super-secret", tok)
	if r != "ok_token" || sub != "dev-alejandro" {
		t.Fatalf("token válido: result=%s subject=%s", r, sub)
	}

	// Firmado con otro secreto → invalid.
	if _, r := ValidateInternalAuth("otro-secreto", tok); r != "invalid" {
		t.Fatalf("token de otro secreto: %s", r)
	}

	// Expirado → expired.
	t.Setenv("INTERNAL_SHARED_SECRET", "super-secret")
	expired, err := NewInternalAuthToken("dev-alejandro", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // exp va en segundos unix
	if _, r := ValidateInternalAuth("super-secret", expired); r != "expired" {
		t.Fatalf("token expirado: %s", r)
	}

	// Subjects inválidos.
	if _, err := NewInternalAuthToken("con.punto", time.Hour); err == nil {
		t.Fatal("subject con punto debería fallar")
	}
	if _, err := NewInternalAuthToken("", time.Hour); err == nil {
		t.Fatal("subject vacío debería fallar")
	}
}

func TestRequireInternalAuthLogOnly(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "super-secret")
	t.Setenv("INTERNAL_AUTH_ENFORCE", "false")
	h := RequireInternalAuth(okHandler())

	if rec := doReq(t, h, "/bookings", ""); rec.Code != http.StatusOK {
		t.Fatalf("log-only sin header debería pasar: %d", rec.Code)
	}
	if rec := doReq(t, h, "/bookings", "super-secret"); rec.Code != http.StatusOK {
		t.Fatalf("log-only con secreto debería pasar: %d", rec.Code)
	}
}

func TestRequireInternalAuthEnforce(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "super-secret")
	t.Setenv("INTERNAL_AUTH_ENFORCE", "true")
	h := RequireInternalAuth(okHandler())

	if rec := doReq(t, h, "/bookings", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("enforce sin header debería dar 403: %d", rec.Code)
	}
	if rec := doReq(t, h, "/bookings", "mal"); rec.Code != http.StatusForbidden {
		t.Fatalf("enforce con secreto malo debería dar 403: %d", rec.Code)
	}
	if rec := doReq(t, h, "/bookings", "super-secret"); rec.Code != http.StatusOK {
		t.Fatalf("enforce con secreto debería pasar: %d", rec.Code)
	}

	tok, err := NewInternalAuthToken("dev-tester", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rec := doReq(t, h, "/bookings", tok); rec.Code != http.StatusOK {
		t.Fatalf("enforce con token válido debería pasar: %d", rec.Code)
	}

	// /health y /metrics siempre exentos.
	if rec := doReq(t, h, "/health", ""); rec.Code != http.StatusOK {
		t.Fatalf("/health debería estar exento: %d", rec.Code)
	}
	if rec := doReq(t, h, "/metrics", ""); rec.Code != http.StatusOK {
		t.Fatalf("/metrics debería estar exento: %d", rec.Code)
	}

	// Exenciones por prefijo (webhooks, contenido público).
	t.Setenv("INTERNAL_AUTH_EXEMPT_PATHS", "/webhooks/stripe, /contents")
	if rec := doReq(t, h, "/webhooks/stripe", ""); rec.Code != http.StatusOK {
		t.Fatalf("webhook exento debería pasar: %d", rec.Code)
	}
	if rec := doReq(t, h, "/contents/abc/track-card", ""); rec.Code != http.StatusOK {
		t.Fatalf("/contents exento debería pasar: %d", rec.Code)
	}
	if rec := doReq(t, h, "/contenidos-no", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("prefijo no exento debería dar 403: %d", rec.Code)
	}
}

func TestRequireInternalAuthEnforceSinSecretoFailClosed(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "")
	t.Setenv("INTERNAL_AUTH_ENFORCE", "true")
	h := RequireInternalAuth(okHandler())
	if rec := doReq(t, h, "/bookings", "lo-que-sea"); rec.Code != http.StatusForbidden {
		t.Fatalf("enforce sin secreto configurado debería cerrar: %d", rec.Code)
	}
}
