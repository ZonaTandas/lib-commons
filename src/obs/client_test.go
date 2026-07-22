package obs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestNewRequestPropagaTraceYAuthInterna(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "s3cr3t")
	ctx := WithTraceID(context.Background(), "trace-123")
	req, err := NewRequest(ctx, http.MethodGet, "http://example.test/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get(HeaderTraceID); got != "trace-123" {
		t.Errorf("X-Trace-Id = %q, esperaba trace-123", got)
	}
	if got := req.Header.Get(HeaderInternalAuth); got != "s3cr3t" {
		t.Errorf("X-Internal-Auth = %q, esperaba el secreto", got)
	}
}

func TestNewRequestSinTraceNiSecreto(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "")
	req, err := NewRequest(context.Background(), http.MethodGet, "http://example.test/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get(HeaderTraceID); got != "" {
		t.Errorf("X-Trace-Id = %q, esperaba vacío", got)
	}
	if got := req.Header.Get(HeaderInternalAuth); got != "" {
		t.Errorf("X-Internal-Auth = %q, esperaba vacío", got)
	}
}

func TestNewRequestMetodoInvalido(t *testing.T) {
	if _, err := NewRequest(context.Background(), "MÉTODO MALO", "http://example.test", nil); err == nil {
		t.Fatal("esperaba error con método inválido")
	}
}

func TestDoPropagaTraceYRegistraStatus(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "s3cr3t")
	var gotTrace, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTrace = r.Header.Get(HeaderTraceID)
		gotAuth = r.Header.Get(HeaderInternalAuth)
		status, _ := strconv.Atoi(r.URL.Query().Get("status"))
		w.WriteHeader(status)
	}))
	defer srv.Close()

	ctx := WithTraceID(context.Background(), "trace-do")
	// Cubre los tres niveles de log (info/warn/error) y el client nil.
	for _, status := range []int{200, 404, 500} {
		// Request construida SIN obs.NewRequest: Do debe poner el trace igualmente.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/x?status="+strconv.Itoa(status), nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := Do(ctx, nil, req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != status {
			t.Errorf("status = %d, esperaba %d", resp.StatusCode, status)
		}
		if gotTrace != "trace-do" {
			t.Errorf("el servidor recibió X-Trace-Id = %q, esperaba trace-do", gotTrace)
		}
		if gotAuth != "s3cr3t" {
			t.Errorf("el servidor recibió X-Internal-Auth = %q, esperaba el secreto", gotAuth)
		}
	}
}

func TestDoErrorDeRed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // el servidor ya no escucha: Do debe devolver error
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Do(context.Background(), srv.Client(), req); err == nil {
		t.Fatal("esperaba error de red con el servidor cerrado")
	}
}
