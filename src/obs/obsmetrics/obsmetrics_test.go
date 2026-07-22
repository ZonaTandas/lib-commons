package obsmetrics

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZonaTandas/lib-commons/src/obs"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMiddlewareCuentaPorTemplate(t *testing.T) {
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/things/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}).Methods(http.MethodPost)

	before := testutil.ToFloat64(requestsTotal.WithLabelValues("/things/{id}", http.MethodPost, "201"))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/things/42", nil))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d", rr.Code)
	}
	after := testutil.ToFloat64(requestsTotal.WithLabelValues("/things/{id}", http.MethodPost, "201"))
	if after != before+1 {
		t.Errorf("counter por template: before=%v after=%v, esperaba +1", before, after)
	}
}

func TestMiddlewareSaltaHealthYMetrics(t *testing.T) {
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/health", "/metrics"} {
		before := testutil.ToFloat64(requestsTotal.WithLabelValues(path, http.MethodGet, "200"))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		after := testutil.ToFloat64(requestsTotal.WithLabelValues(path, http.MethodGet, "200"))
		if after != before {
			t.Errorf("%s no debe instrumentarse", path)
		}
	}
}

func TestHandlerExponeMetricas(t *testing.T) {
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "go_goroutines") {
		t.Errorf("status = %d, esperaba exposición del registry por defecto", rr.Code)
	}
}

func TestTokenHandler(t *testing.T) {
	cases := []struct {
		name   string
		env    string
		header string
		want   int
	}{
		{"env vacía cierra siempre", "", "Bearer lo-que-sea", http.StatusUnauthorized},
		{"token incorrecto", "tok", "Bearer malo", http.StatusUnauthorized},
		{"sin cabecera", "tok", "", http.StatusUnauthorized},
		{"token correcto", "tok", "Bearer tok", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("AUTH_SERVICE_TOKEN", c.env)
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rr := httptest.NewRecorder()
			TokenHandler().ServeHTTP(rr, req)
			if rr.Code != c.want {
				t.Errorf("status = %d, esperaba %d", rr.Code, c.want)
			}
		})
	}
}

func TestObserverInternalAuthRegistradoEnInit(t *testing.T) {
	// init() engancha el counter a obs.RequireInternalAuth: una request sin
	// cabecera debe incrementar internal_auth_requests_total{result="missing"}.
	t.Setenv("INTERNAL_SHARED_SECRET", "sec")
	h := obs.RequireInternalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	before := testutil.ToFloat64(internalAuthTotal.WithLabelValues("missing", "/ruta-observer"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ruta-observer", nil))
	after := testutil.ToFloat64(internalAuthTotal.WithLabelValues("missing", "/ruta-observer"))
	if after != before+1 {
		t.Errorf("counter internal_auth: before=%v after=%v, esperaba +1", before, after)
	}
}

type hijackerFalso struct{ http.ResponseWriter }

func (hijackerFalso) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }

type flusherEspia struct {
	http.ResponseWriter
	flushed bool
}

func (f *flusherEspia) Flush() { f.flushed = true }

func TestStatusRecorderDelegaciones(t *testing.T) {
	base := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: base, status: http.StatusOK}

	// Write sin WriteHeader explícito: status implícito 200.
	if _, err := rec.Write([]byte("x")); err != nil || rec.status != http.StatusOK {
		t.Errorf("Write implícito: err=%v status=%d", err, rec.status)
	}
	// WriteHeader posterior no machaca el primero.
	rec.WriteHeader(http.StatusTeapot)
	if rec.status != http.StatusOK {
		t.Errorf("status = %d, el primer WriteHeader/Write manda", rec.status)
	}

	if rec.Unwrap() != http.ResponseWriter(base) {
		t.Error("Unwrap no devuelve el ResponseWriter subyacente")
	}
	if _, _, err := rec.Hijack(); err == nil {
		t.Error("esperaba error de Hijack sobre recorder no hijackeable")
	}
	rec2 := &statusRecorder{ResponseWriter: hijackerFalso{base}}
	if _, _, err := rec2.Hijack(); err != nil {
		t.Errorf("Hijack debía delegar, err = %v", err)
	}
	espia := &flusherEspia{ResponseWriter: base}
	rec3 := &statusRecorder{ResponseWriter: espia}
	rec3.Flush()
	if !espia.flushed {
		t.Error("Flush no delegó en el Flusher subyacente")
	}
	// Flush sobre un writer sin Flusher: no hace nada ni casca.
	(&statusRecorder{ResponseWriter: hijackerFalso{base}}).Flush()
}
