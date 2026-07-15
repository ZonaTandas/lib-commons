package obs

import (
	"context"
	"regexp"
	"testing"
)

func TestNewTraceIDFormat(t *testing.T) {
	uuidV4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewTraceID()
		if !uuidV4.MatchString(id) {
			t.Fatalf("no es un uuid v4: %q", id)
		}
		if seen[id] {
			t.Fatalf("uuid repetido: %q", id)
		}
		seen[id] = true
	}
}

func TestTraceIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := TraceID(ctx); got != "" {
		t.Fatalf("context vacío debería dar \"\", dio %q", got)
	}
	ctx = WithTraceID(ctx, "abc-123")
	if got := TraceID(ctx); got != "abc-123" {
		t.Fatalf("TraceID = %q", got)
	}
}

func TestAddWithoutBagIsNoop(t *testing.T) {
	// Fuera de una request (sin middleware) Add no debe romper nada.
	Add(context.Background(), "pnr", "ABC123")
	Logger(context.Background()).Info("no panic")
}

func TestDetachCopiesTraceAndFields(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = withFieldBag(WithTraceID(ctx, "trace-1"))
	Add(ctx, "pnr", "ABC123")

	dctx := Detach(ctx)
	cancel()

	if err := dctx.Err(); err != nil {
		t.Fatalf("el context detached no debe cancelarse: %v", err)
	}
	if got := TraceID(dctx); got != "trace-1" {
		t.Fatalf("traceId no sobrevivió al Detach: %q", got)
	}
	// La copia es independiente: lo añadido tras el Detach no cruza.
	Add(ctx, "soloRequest", true)
	Add(dctx, "soloGoroutine", true)
	reqFields := fieldsSnapshot(ctx)
	detFields := fieldsSnapshot(dctx)
	if len(reqFields) != 2 || len(detFields) != 2 {
		t.Fatalf("bolsas no independientes: request=%v detached=%v", reqFields, detFields)
	}
}

func TestSanitizeTraceID(t *testing.T) {
	if sanitizeTraceID("test-123") != "test-123" {
		t.Fatal("un id normal debe pasar")
	}
	for _, bad := range []string{"", "con espacios", "salto\nde-linea", "x\"y", string(make([]byte, 100))} {
		if sanitizeTraceID(bad) != "" {
			t.Fatalf("id inválido aceptado: %q", bad)
		}
	}
}
