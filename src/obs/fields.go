package obs

import (
	"context"
	"log/slog"
	"sort"
	"sync"
)

// Bolsa mutable de campos de negocio en el context (puntero + mutex, mismo
// patrón que el context.WithValue de los middleware/auth.go de los
// servicios): el middleware la instala y cualquier capa posterior puede
// añadir pnr/bookingId/userId... que salen en la línea de acceso final y en
// todo Logger(ctx) posterior.
type fieldBag struct {
	mu sync.Mutex
	kv map[string]any
}

type ctxKeyFields struct{}

func withFieldBag(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyFields{}, &fieldBag{kv: map[string]any{}})
}

// WithFieldBag instala una bolsa vacía fuera del Middleware HTTP (consumers
// AMQP, jobs): a partir de aquí obs.Add funciona con normalidad.
func WithFieldBag(ctx context.Context) context.Context { return withFieldBag(ctx) }

func bagFrom(ctx context.Context) *fieldBag {
	b, _ := ctx.Value(ctxKeyFields{}).(*fieldBag)
	return b
}

// Add acumula un campo de negocio en el context (no-op si el middleware no
// instaló la bolsa, p. ej. en tests o fuera de una request).
func Add(ctx context.Context, key string, value any) {
	if b := bagFrom(ctx); b != nil {
		b.mu.Lock()
		b.kv[key] = value
		b.mu.Unlock()
	}
}

// snapshot devuelve una copia ordenada de los campos acumulados.
func fieldsSnapshot(ctx context.Context) []slog.Attr {
	b := bagFrom(ctx)
	if b == nil {
		return nil
	}
	b.mu.Lock()
	keys := make([]string, 0, len(b.kv))
	for k := range b.kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	attrs := make([]slog.Attr, 0, len(keys))
	for _, k := range keys {
		attrs = append(attrs, slog.Any(k, b.kv[k]))
	}
	b.mu.Unlock()
	return attrs
}

// Logger devuelve un *slog.Logger con el traceId y los campos de negocio
// acumulados hasta ahora. Sustituye a los fmt.Println de los servicios:
//
//	obs.Logger(r.Context()).Info("booking confirmada", "total", total)
func Logger(ctx context.Context) *slog.Logger {
	logger := slog.Default()
	if tid := TraceID(ctx); tid != "" {
		logger = logger.With("traceId", tid)
	}
	for _, a := range fieldsSnapshot(ctx) {
		logger = logger.With(a)
	}
	return logger
}
