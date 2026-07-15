package obs

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Política de captura de cuerpos (OBS_CAPTURE_BODIES):
//   - "writes+errors" (default): cuerpos de petición/respuesta en TODAS las
//     escrituras (POST/PUT/PATCH/DELETE) y en todo error 4xx/5xx.
//   - "errors": solo en errores.
//   - "off": nunca (válvula de escape si el volumen de Loki se dispara).
//
// Los cuerpos van truncados a OBS_MAX_BODY_BYTES (8192) y SIEMPRE pasados por
// MaskJSON (jamás tokens/secretos; DNI/tarjeta parcializados).
type obsConfig struct {
	maxBodyBytes int
	captureMode  string
	samplePaths  []string
}

func loadConfig() obsConfig {
	cfg := obsConfig{maxBodyBytes: 8192, captureMode: "writes+errors"}
	if v := os.Getenv("OBS_MAX_BODY_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.maxBodyBytes = n
		}
	}
	switch os.Getenv("OBS_CAPTURE_BODIES") {
	case "errors":
		cfg.captureMode = "errors"
	case "off":
		cfg.captureMode = "off"
	}
	if v := os.Getenv("OBS_SAMPLE_PATHS"); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				cfg.samplePaths = append(cfg.samplePaths, p)
			}
		}
	}
	return cfg
}

var traceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func sanitizeTraceID(raw string) string {
	if traceIDPattern.MatchString(raw) {
		return raw
	}
	return ""
}

// Middleware extrae/genera el X-Trace-Id, lo devuelve en la respuesta,
// instala la bolsa de campos de negocio y al acabar emite la línea de acceso
// http_request. Envuelve el ResponseWriter para capturar status y cuerpo
// (≤OBS_MAX_BODY_BYTES) implementando Flusher/Hijacker (el SSE de
// booking-queue sigue funcionando). Salta /health y /metrics; las rutas con
// prefijo en OBS_SAMPLE_PATHS (availability, SSE) solo se loguean si fallan.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		cfg := loadConfig()
		tid := sanitizeTraceID(r.Header.Get(HeaderTraceID))
		if tid == "" {
			tid = NewTraceID()
		}
		ctx := withFieldBag(WithTraceID(r.Context(), tid))
		r = r.WithContext(ctx)
		// La cabecera se fija ANTES del handler para que también llegue en
		// respuestas en streaming y en errores (código de incidencia del wizard).
		w.Header().Set(HeaderTraceID, tid)

		isWrite := r.Method == http.MethodPost || r.Method == http.MethodPut ||
			r.Method == http.MethodPatch || r.Method == http.MethodDelete

		var reqBuf *limitedBuffer
		if cfg.captureMode != "off" && isWrite && r.Body != nil {
			reqBuf = newLimitedBuffer(cfg.maxBodyBytes)
			r.Body = &teeReadCloser{rc: r.Body, buf: reqBuf}
		}

		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		if cfg.captureMode != "off" {
			rec.buf = newLimitedBuffer(cfg.maxBodyBytes)
		}

		start := time.Now()
		next.ServeHTTP(rec, r)
		durationMs := time.Since(start).Milliseconds()

		isError := rec.status >= 400
		if !isError && pathSampled(cfg.samplePaths, r.URL.Path) {
			return
		}

		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		} else if isError {
			level = slog.LevelWarn
		}

		attrs := []slog.Attr{
			slog.String("traceId", tid),
			slog.String("method", r.Method),
			slog.String("route", RouteTemplate(r)),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int64("durationMs", durationMs),
		}
		attrs = append(attrs, fieldsSnapshot(ctx)...)

		logBodies := cfg.captureMode == "writes+errors" && isWrite ||
			cfg.captureMode != "off" && isError
		if logBodies {
			if reqBuf != nil && reqBuf.Len() > 0 {
				attrs = append(attrs, slog.String("reqBody", string(MaskJSON(reqBuf.Bytes()))))
				if reqBuf.truncated {
					attrs = append(attrs, slog.Bool("reqBodyTruncated", true))
				}
			}
			if rec.buf != nil && rec.buf.Len() > 0 {
				attrs = append(attrs, slog.String("respBody", string(MaskJSON(rec.buf.Bytes()))))
				if rec.buf.truncated {
					attrs = append(attrs, slog.Bool("respBodyTruncated", true))
				}
			}
		}

		slog.Default().LogAttrs(ctx, level, "http_request", attrs...)
	})
}

// RouteTemplate devuelve el template de gorilla/mux de la request
// (/bookings/{id}) o, si no lo hay, el path real. Para métricas usar SIEMPRE
// el template (cardinalidad finita); en logs el path real es un campo más.
func RouteTemplate(r *http.Request) string {
	if route := mux.CurrentRoute(r); route != nil {
		if tpl, err := route.GetPathTemplate(); err == nil && tpl != "" {
			return tpl
		}
	}
	return r.URL.Path
}

// pathSampled: cada entrada de OBS_SAMPLE_PATHS es un prefijo de ruta, o un
// sufijo si empieza por "*" (p. ej. "*/availability" para
// /activities/{id}/availability, que no se puede expresar por prefijo).
func pathSampled(patterns []string, path string) bool {
	for _, p := range patterns {
		if strings.HasPrefix(p, "*") {
			if strings.HasSuffix(path, p[1:]) {
				return true
			}
		} else if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// limitedBuffer acumula hasta max bytes y marca el truncado.
type limitedBuffer struct {
	max       int
	buf       bytes.Buffer
	truncated bool
}

func newLimitedBuffer(max int) *limitedBuffer { return &limitedBuffer{max: max} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
			b.truncated = true
		} else {
			b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) Len() int      { return b.buf.Len() }
func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

// teeReadCloser copia lo leído del body a un buffer limitado sin robarle el
// Close al handler.
type teeReadCloser struct {
	rc  io.ReadCloser
	buf *limitedBuffer
}

func (t *teeReadCloser) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		t.buf.Write(p[:n])
	}
	return n, err
}

func (t *teeReadCloser) Close() error { return t.rc.Close() }

// responseRecorder captura status y cuerpo. Implementa Flusher/Hijacker
// delegando en el ResponseWriter real (imprescindible para SSE y websockets)
// y Unwrap para http.ResponseController.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	buf         *limitedBuffer
}

func (r *responseRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	if r.buf != nil {
		r.buf.Write(p)
	}
	return r.ResponseWriter.Write(p)
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("obs: el ResponseWriter subyacente no soporta Hijack")
}

func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
