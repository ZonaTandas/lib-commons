package obs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// NewRequest es el sustituto de http.NewRequest en los clientes
// inter-servicio (httpserver/controllers/shared/*Service.go): engancha el
// context y propaga el X-Trace-Id.
func NewRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if tid := TraceID(ctx); tid != "" {
		req.Header.Set(HeaderTraceID, tid)
	}
	return req, nil
}

// Do ejecuta la request y loguea la llamada saliente (msg http_out) con
// target, método, status y duración. client nil = http.DefaultClient.
func Do(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	// Por si la request no vino de obs.NewRequest:
	if req.Header.Get(HeaderTraceID) == "" {
		if tid := TraceID(ctx); tid != "" {
			req.Header.Set(HeaderTraceID, tid)
		}
	}
	start := time.Now()
	resp, err := client.Do(req)
	durationMs := time.Since(start).Milliseconds()

	logger := Logger(ctx)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "http_out",
			slog.String("target", req.URL.Host),
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Int64("durationMs", durationMs),
			slog.String("error", err.Error()),
		)
		return nil, err
	}
	level := slog.LevelInfo
	if resp.StatusCode >= 500 {
		level = slog.LevelError
	} else if resp.StatusCode >= 400 {
		level = slog.LevelWarn
	}
	logger.LogAttrs(ctx, level, "http_out",
		slog.String("target", req.URL.Host),
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("durationMs", durationMs),
	)
	return resp, nil
}
