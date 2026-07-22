package obs

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestInitNiveles(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)

	cases := []struct {
		env     string
		enabled slog.Level
		muted   slog.Level // un nivel por debajo que NO debe estar habilitado (LevelDebug-4 = nunca)
	}{
		{"", slog.LevelInfo, slog.LevelDebug},
		{"info", slog.LevelInfo, slog.LevelDebug},
		{"debug", slog.LevelDebug, slog.LevelDebug - 4},
		{"warn", slog.LevelWarn, slog.LevelInfo},
		{"warning", slog.LevelWarn, slog.LevelInfo},
		{"error", slog.LevelError, slog.LevelWarn},
	}
	ctx := context.Background()
	for _, c := range cases {
		t.Setenv("OBS_LOG_LEVEL", c.env)
		Init("test-svc")
		if !slog.Default().Enabled(ctx, c.enabled) {
			t.Errorf("OBS_LOG_LEVEL=%q: nivel %v debería estar habilitado", c.env, c.enabled)
		}
		if slog.Default().Enabled(ctx, c.muted) {
			t.Errorf("OBS_LOG_LEVEL=%q: nivel %v debería estar silenciado", c.env, c.muted)
		}
	}
}

func TestWithFieldBagYLoggerConCampos(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))

	ctx := WithFieldBag(context.Background())
	ctx = WithTraceID(ctx, "tid-logger")
	Add(ctx, "bookingId", "b-1")
	Logger(ctx).Info("mensaje")

	out := buf.String()
	for _, want := range []string{"tid-logger", "bookingId", "b-1", "mensaje"} {
		if !strings.Contains(out, want) {
			t.Errorf("el log no contiene %q: %s", want, out)
		}
	}
}
