package obs

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// captura las líneas JSON que emitiría stdout
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)).With("service", "test-service"))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func lastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("línea de log no es JSON: %v\n%s", err, buf.String())
	}
	return m
}

func TestMiddlewareEchoesAndGeneratesTraceID(t *testing.T) {
	buf := captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/bookings/{id}", func(w http.ResponseWriter, r *http.Request) {
		Add(r.Context(), "bookingId", mux.Vars(r)["id"])
		w.WriteHeader(http.StatusOK)
	}).Methods(http.MethodGet)

	// Con X-Trace-Id entrante: se respeta y se devuelve.
	req := httptest.NewRequest(http.MethodGet, "/bookings/b-1", nil)
	req.Header.Set(HeaderTraceID, "test-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if got := rec.Header().Get(HeaderTraceID); got != "test-123" {
		t.Fatalf("la respuesta no devuelve el trace entrante: %q", got)
	}
	m := lastLogLine(t, buf)
	if m["msg"] != "http_request" || m["traceId"] != "test-123" {
		t.Fatalf("línea de acceso incorrecta: %v", m)
	}
	if m["route"] != "/bookings/{id}" {
		t.Fatalf("route debe ser el template de mux: %v", m["route"])
	}
	if m["bookingId"] != "b-1" {
		t.Fatalf("el campo de negocio no salió en la línea: %v", m)
	}

	// Sin X-Trace-Id: se genera uno.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bookings/b-2", nil))
	if rec.Header().Get(HeaderTraceID) == "" {
		t.Fatal("sin trace entrante debe generarse uno")
	}
}

func TestMiddlewareCapturesAndMasksBodies(t *testing.T) {
	buf := captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		_ = json.NewDecoder(r.Body).Decode(&v)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true,"token":"jwt-super-secreto"}`))
	}).Methods(http.MethodPost)

	body := `{"user":"ana","password":"hunter2","dni":"12345678Z"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	m := lastLogLine(t, buf)
	reqBody, _ := m["reqBody"].(string)
	if !strings.Contains(reqBody, `"password":"***"`) {
		t.Fatalf("password sin redactar: %s", reqBody)
	}
	if strings.Contains(reqBody, "hunter2") || strings.Contains(reqBody, "12345678Z") {
		t.Fatalf("secreto o dni completo en el log: %s", reqBody)
	}
	if !strings.Contains(reqBody, `"user":"ana"`) {
		t.Fatalf("campo normal perdido: %s", reqBody)
	}
	respBody, _ := m["respBody"].(string)
	if !strings.Contains(respBody, `"token":"***"`) || strings.Contains(respBody, "jwt-super-secreto") {
		t.Fatalf("token de respuesta sin redactar: %s", respBody)
	}
}

func TestMiddlewareReadsGetWithoutBodies(t *testing.T) {
	buf := captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/things", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[1,2,3]}`))
	}).Methods(http.MethodGet)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things", nil))
	m := lastLogLine(t, buf)
	if _, ok := m["respBody"]; ok {
		t.Fatalf("una lectura OK no debe loguear cuerpos: %v", m)
	}
}

func TestMiddlewareLogsBodiesOnErrorReads(t *testing.T) {
	buf := captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/things", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}).Methods(http.MethodGet)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things", nil))
	m := lastLogLine(t, buf)
	if m["level"] != "ERROR" {
		t.Fatalf("un 500 debe loguearse a nivel error: %v", m)
	}
	if respBody, _ := m["respBody"].(string); !strings.Contains(respBody, "boom") {
		t.Fatalf("el cuerpo del error debe capturarse: %v", m)
	}
}

func TestMiddlewareSamplePathsOnlyLogErrors(t *testing.T) {
	t.Setenv("OBS_SAMPLE_PATHS", "/availability")
	buf := captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/availability/{id}", func(w http.ResponseWriter, r *http.Request) {
		if mux.Vars(r)["id"] == "bad" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/availability/a-1", nil))
	if strings.TrimSpace(buf.String()) != "" {
		t.Fatalf("una ruta muestreada OK no debe loguear: %s", buf.String())
	}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/availability/bad", nil))
	if strings.TrimSpace(buf.String()) == "" {
		t.Fatal("los errores de rutas muestreadas SÍ se loguean")
	}
}

func TestMiddlewareSkipsHealthAndMetrics(t *testing.T) {
	buf := captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	if strings.TrimSpace(buf.String()) != "" {
		t.Fatalf("/health no debe loguearse: %s", buf.String())
	}
}

func TestResponseRecorderSupportsFlusher(t *testing.T) {
	// El SSE de booking-queue necesita http.Flusher a través del wrapper.
	captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	flushed := false
	router.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("el wrapper no implementa http.Flusher")
		}
		_, _ = w.Write([]byte("data: hola\n\n"))
		f.Flush()
		flushed = true
	})
	rec := httptest.NewRecorder() // httptest.ResponseRecorder implementa Flusher
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if !flushed || !rec.Flushed {
		t.Fatal("el Flush no llegó al ResponseWriter real")
	}
}

func TestMiddlewareCaptureBodiesOff(t *testing.T) {
	t.Setenv("OBS_CAPTURE_BODIES", "off")
	buf := captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/w", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"x"}`))
	}).Methods(http.MethodPost)

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/w", strings.NewReader(`{"a":1}`)))
	m := lastLogLine(t, buf)
	if _, ok := m["reqBody"]; ok {
		t.Fatalf("con OBS_CAPTURE_BODIES=off no debe haber cuerpos: %v", m)
	}
	if _, ok := m["respBody"]; ok {
		t.Fatalf("con OBS_CAPTURE_BODIES=off no debe haber cuerpos: %v", m)
	}
}

func TestBodyTruncation(t *testing.T) {
	t.Setenv("OBS_MAX_BODY_BYTES", "16")
	buf := captureLogs(t)
	router := mux.NewRouter()
	router.Use(Middleware)
	router.HandleFunc("/w", func(w http.ResponseWriter, r *http.Request) {
		// Solo se captura lo que el handler LEE (TeeReader): leemos el body
		// como haría cualquier handler real decodificando JSON.
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}).Methods(http.MethodPost)

	big := `{"data":"` + strings.Repeat("x", 100) + `"}`
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/w", strings.NewReader(big)))
	m := lastLogLine(t, buf)
	if m["reqBodyTruncated"] != true {
		t.Fatalf("cuerpo grande debe marcar truncado: %v", m)
	}
	// Truncado a 16 bytes ya no es JSON válido → se omite enmascarado entero.
	if reqBody, _ := m["reqBody"].(string); !strings.Contains(reqBody, "omitido") {
		t.Fatalf("un JSON truncado no parseable debe omitirse: %v", m["reqBody"])
	}
}
