package obs

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPathSampledPrefijoYSufijo(t *testing.T) {
	patterns := []string{"/sse", "*/availability"}
	cases := []struct {
		path string
		want bool
	}{
		{"/sse/stream", true},
		{"/activities/3/availability", true},
		{"/bookings", false},
	}
	for _, c := range cases {
		if got := pathSampled(patterns, c.path); got != c.want {
			t.Errorf("pathSampled(%q) = %v, esperaba %v", c.path, got, c.want)
		}
	}
}

func TestLoadConfigValoresInvalidos(t *testing.T) {
	t.Setenv("OBS_MAX_BODY_BYTES", "abc")
	if cfg := loadConfig(); cfg.maxBodyBytes != 8192 {
		t.Errorf("maxBodyBytes con valor no numérico = %d, esperaba 8192", cfg.maxBodyBytes)
	}
	t.Setenv("OBS_MAX_BODY_BYTES", "-5")
	if cfg := loadConfig(); cfg.maxBodyBytes != 8192 {
		t.Errorf("maxBodyBytes negativo = %d, esperaba 8192", cfg.maxBodyBytes)
	}
	t.Setenv("OBS_MAX_BODY_BYTES", "16")
	t.Setenv("OBS_CAPTURE_BODIES", "errors")
	t.Setenv("OBS_SAMPLE_PATHS", " /a , ,/b ")
	cfg := loadConfig()
	if cfg.maxBodyBytes != 16 || cfg.captureMode != "errors" {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(cfg.samplePaths) != 2 || cfg.samplePaths[0] != "/a" || cfg.samplePaths[1] != "/b" {
		t.Errorf("samplePaths = %v, esperaba [/a /b]", cfg.samplePaths)
	}
}

func TestLimitedBufferTruncado(t *testing.T) {
	b := newLimitedBuffer(4)
	if n, _ := b.Write([]byte("abcdef")); n != 6 {
		t.Errorf("Write debe declarar los bytes de entrada, n = %d", n)
	}
	if b.Len() != 4 || !b.truncated || string(b.Bytes()) != "abcd" {
		t.Errorf("buffer = %q truncated=%v", b.Bytes(), b.truncated)
	}
	// Con el buffer lleno, escrituras posteriores no crecen.
	if n, _ := b.Write([]byte("x")); n != 1 || b.Len() != 4 {
		t.Errorf("tras overflow: n=%d len=%d", n, b.Len())
	}
}

type closerEspia struct {
	io.Reader
	cerrado bool
}

func (c *closerEspia) Close() error { c.cerrado = true; return nil }

func TestTeeReadCloserClose(t *testing.T) {
	espia := &closerEspia{Reader: strings.NewReader("hola")}
	tee := &teeReadCloser{rc: espia, buf: newLimitedBuffer(8)}
	if _, err := io.ReadAll(tee); err != nil {
		t.Fatal(err)
	}
	if err := tee.Close(); err != nil || !espia.cerrado {
		t.Errorf("Close no delegó en el ReadCloser subyacente (err=%v, cerrado=%v)", err, espia.cerrado)
	}
}

type hijackerFalso struct{ http.ResponseWriter }

func (hijackerFalso) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }

func TestResponseRecorderHijackYUnwrap(t *testing.T) {
	base := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: base}
	if _, _, err := rec.Hijack(); err == nil {
		t.Error("esperaba error de Hijack sobre un ResponseWriter no hijackeable")
	}
	if rec.Unwrap() != http.ResponseWriter(base) {
		t.Error("Unwrap no devuelve el ResponseWriter subyacente")
	}
	rec2 := &responseRecorder{ResponseWriter: hijackerFalso{base}}
	if _, _, err := rec2.Hijack(); err != nil {
		t.Errorf("Hijack debía delegar sin error, err = %v", err)
	}
}

func TestMiddlewareRespuestaTruncada(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))

	t.Setenv("OBS_MAX_BODY_BYTES", "8")
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"campo":"una respuesta bastante larga"}`))
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":"cuerpo largo de peticion"}`)))

	out := buf.String()
	if !strings.Contains(out, "reqBodyTruncated") || !strings.Contains(out, "respBodyTruncated") {
		t.Errorf("esperaba marcas de truncado en el log: %s", out)
	}
}
