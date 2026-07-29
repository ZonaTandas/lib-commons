package authn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZonaTandas/lib-commons/src/obs"
)

func TestBearerToken(t *testing.T) {
	cases := map[string]string{
		"Bearer abc":        "abc",
		"Bearer Bearer abc": "abc", // doble prefijo: lo mandaba algún BFF
		"  Bearer   abc  ":  "abc",
		"abc":               "abc",
		"":                  "",
		"Bearer ":           "",
		"Bearer":            "",
	}
	for in, want := range cases {
		if got := BearerToken(in); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", in, got, want)
		}
	}
}

// La razón de existir del helper: la llamada a /verify debe llevar traceId y
// X-Internal-Auth. Las copias por servicio ponían el segundo pero no el
// primero, y el salto a oauth desaparecía de la traza.
func TestVerifySessionPropagaTraceIdYInternalAuth(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "s3cr3t")

	var gotTrace, gotInternal, gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTrace = r.Header.Get(obs.HeaderTraceID)
		gotInternal = r.Header.Get(obs.HeaderInternalAuth)
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u-1","email":"A@B.com","emailVerified":true}`))
	}))
	defer srv.Close()

	ctx := obs.WithTraceID(context.Background(), "trace-123")
	user, err := VerifySession(ctx, srv.URL, "sess-1", "svc-token")
	if err != nil {
		t.Fatalf("err inesperado: %v", err)
	}
	if gotTrace != "trace-123" {
		t.Errorf("traceId = %q, want trace-123", gotTrace)
	}
	if gotInternal != "s3cr3t" {
		t.Errorf("X-Internal-Auth = %q, want s3cr3t", gotInternal)
	}
	if gotAuth != "Bearer svc-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotPath != "/verify/sess-1" {
		t.Errorf("path = %q", gotPath)
	}
	if user.ID != "u-1" || user.Email != "a@b.com" || !user.EmailVerified {
		t.Errorf("user = %+v (el email debe normalizarse a minúsculas)", user)
	}
}

func TestVerifySessionDistingueSesionInvalidaDeCaidaDeOauth(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"401 sesión caducada", http.StatusUnauthorized, ErrInvalidSession},
		{"404 sesión inexistente", http.StatusNotFound, ErrInvalidSession},
		{"403", http.StatusForbidden, ErrInvalidSession},
		{"500 oauth roto", http.StatusInternalServerError, ErrUpstream},
		{"503 oauth caído", http.StatusServiceUnavailable, ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			_, err := VerifySession(context.Background(), srv.URL, "s", "t")
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestVerifySessionErroresDeEntrada(t *testing.T) {
	if _, err := VerifySession(context.Background(), "http://x", "  ", "t"); !errors.Is(err, ErrNoSession) {
		t.Errorf("token vacío: err = %v, want ErrNoSession", err)
	}
	if _, err := VerifySession(context.Background(), "", "s", "t"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("base vacía: err = %v, want ErrNotConfigured", err)
	}
	if _, err := VerifySession(context.Background(), "://bad url", "s", "t"); !errors.Is(err, ErrUpstream) {
		t.Errorf("url inválida: err = %v, want ErrUpstream", err)
	}
}

func TestVerifySessionRespuestaIlegibleOSinIdEsUpstream(t *testing.T) {
	for _, body := range []string{`no json`, `{"id":"","email":"x"}`, `{"id":"u","email":""}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := VerifySession(context.Background(), srv.URL, "s", "t")
		if !errors.Is(err, ErrUpstream) {
			t.Errorf("body %q: err = %v, want ErrUpstream", body, err)
		}
		srv.Close()
	}
}

func TestVerifySessionRedCaida(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // el destino ya no escucha
	if _, err := VerifySession(context.Background(), url, "s", "t"); !errors.Is(err, ErrUpstream) {
		t.Errorf("err = %v, want ErrUpstream", err)
	}
}

func TestVerifySessionSinServiceTokenNoMandaAuthorization(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"id":"u","email":"e@x.es"}`))
	}))
	defer srv.Close()
	if _, err := VerifySessionWithClient(context.Background(), nil, srv.URL, "s", ""); err != nil {
		t.Fatalf("err: %v", err)
	}
	if hadAuth {
		t.Error("no debería mandar Authorization vacío")
	}
}
