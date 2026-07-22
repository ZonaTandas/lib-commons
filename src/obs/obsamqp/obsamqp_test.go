package obsamqp

import (
	"context"
	"regexp"
	"testing"

	"github.com/ZonaTandas/lib-commons/src/obs"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestInjectConYSinTrace(t *testing.T) {
	ctx := obs.WithTraceID(context.Background(), "tid-amqp")
	h := Inject(ctx, nil)
	if h[HeaderTraceID] != "tid-amqp" {
		t.Errorf("header = %v, esperaba tid-amqp", h[HeaderTraceID])
	}

	// Sin trace en el context: la tabla existente se devuelve intacta.
	existing := amqp.Table{"otra": "clave"}
	h2 := Inject(context.Background(), existing)
	if _, ok := h2[HeaderTraceID]; ok {
		t.Error("no debía inyectar trace sin traceId en el context")
	}
	if h2["otra"] != "clave" {
		t.Error("la tabla existente debe conservarse")
	}
}

func TestExtractRoundTripYGeneracion(t *testing.T) {
	h := Inject(obs.WithTraceID(context.Background(), "tid-rt"), nil)
	ctx := Extract(context.Background(), h)
	if got := obs.TraceID(ctx); got != "tid-rt" {
		t.Errorf("round-trip = %q, esperaba tid-rt", got)
	}

	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	// Sin headers: genera un trace nuevo.
	if got := obs.TraceID(Extract(context.Background(), nil)); !uuid.MatchString(got) {
		t.Errorf("sin headers debía generar UUID, got %q", got)
	}
	// Header con tipo no-string: también genera.
	if got := obs.TraceID(Extract(context.Background(), amqp.Table{HeaderTraceID: 5})); !uuid.MatchString(got) {
		t.Errorf("header no-string debía generar UUID, got %q", got)
	}
}

func TestExtractInstalaBolsaDeCampos(t *testing.T) {
	ctx := Extract(context.Background(), nil)
	// La bolsa está instalada: Add no es no-op y el campo sobrevive a Detach.
	obs.Add(ctx, "clave", "valor")
	if obs.TraceID(obs.Detach(ctx)) != obs.TraceID(ctx) {
		t.Error("Detach debe conservar el traceId de la bolsa del consumer")
	}
}
