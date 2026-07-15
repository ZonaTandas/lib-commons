package obs

import "context"

// Detach devuelve un context para goroutines best-effort (emails, refunds,
// sync a profiles...): sobrevive a la cancelación de la request
// (context.WithoutCancel) y lleva una COPIA del traceId y de los campos de
// negocio — los Add posteriores de la goroutine no ensucian la request y
// viceversa.
//
// OJO: no añade cancelación propia; la goroutine depende de los timeouts de
// su http.Client, como hasta ahora.
//
//	dctx := obs.Detach(r.Context()) // SIEMPRE antes del `go func`
//	go func() { ... obs.Logger(dctx).Info(...) ... }()
func Detach(ctx context.Context) context.Context {
	d := context.WithoutCancel(ctx)
	if b := bagFrom(ctx); b != nil {
		b.mu.Lock()
		kv := make(map[string]any, len(b.kv))
		for k, v := range b.kv {
			kv[k] = v
		}
		b.mu.Unlock()
		d = context.WithValue(d, ctxKeyFields{}, &fieldBag{kv: kv})
	}
	return d
}
