package obs

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestSetInternalAuthRespetaCabeceraYSecreto(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.test", nil)

	t.Setenv("INTERNAL_SHARED_SECRET", "")
	SetInternalAuth(req)
	if got := req.Header.Get(HeaderInternalAuth); got != "" {
		t.Errorf("sin secreto configurado no debe fijar cabecera, got %q", got)
	}

	t.Setenv("INTERNAL_SHARED_SECRET", "sec")
	SetInternalAuth(req)
	if got := req.Header.Get(HeaderInternalAuth); got != "sec" {
		t.Errorf("cabecera = %q, esperaba sec", got)
	}

	// Una cabecera ya presente (p. ej. un token caducable) no se machaca.
	req.Header.Set(HeaderInternalAuth, "token-previo")
	SetInternalAuth(req)
	if got := req.Header.Get(HeaderInternalAuth); got != "token-previo" {
		t.Errorf("cabecera = %q, no debía sobrescribirse", got)
	}
}

func TestNewInternalAuthTokenErrores(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "")
	if _, err := NewInternalAuthToken("dev", time.Hour); err == nil {
		t.Error("esperaba error sin INTERNAL_SHARED_SECRET")
	}

	t.Setenv("INTERNAL_SHARED_SECRET", "sec")
	for _, subject := range []string{"", "con.punto", "con espacio", "con\ttab", "con\nsalto"} {
		if _, err := NewInternalAuthToken(subject, time.Hour); err == nil {
			t.Errorf("esperaba error con subject %q", subject)
		}
	}
	if _, err := NewInternalAuthToken("dev", 0); err == nil {
		t.Error("esperaba error con ttl 0")
	}
}

func TestValidateInternalAuthExpNoNumerica(t *testing.T) {
	// Token bien firmado pero con exp no numérica: inválido.
	payload := "v1.dev.no-numero"
	tok := payload + "." + internalTokenSign("sec", payload)
	if _, result := ValidateInternalAuth("sec", tok); result != "invalid" {
		t.Errorf("result = %q, esperaba invalid", result)
	}
}

func TestRequireInternalAuthExentosYObserver(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "sec")
	t.Setenv("INTERNAL_AUTH_EXEMPT_PATHS", "/contents, /public")

	var results []string
	SetInternalAuthObserver(func(result, route string) { results = append(results, result) })
	defer SetInternalAuthObserver(nil)

	h := RequireInternalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, c := range []struct {
		path string
		want string
	}{
		{"/contents/track-1/img.png", "exempt"},
		{"/public/x", "exempt"},
		{"/health", "exempt"},
		{"/bookings", "missing"}, // log-only por defecto: pasa igualmente
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("%s: status = %d, esperaba 200 (log-only)", c.path, rr.Code)
		}
	}
	want := []string{"exempt", "exempt", "exempt", "missing"}
	if !slices.Equal(results, want) {
		t.Errorf("observer registró %v, esperaba %v", results, want)
	}
}
