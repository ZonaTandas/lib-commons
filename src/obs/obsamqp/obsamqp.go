// Package obsamqp propaga el traceId por RabbitMQ mediante el header AMQP
// x-trace-id. El publicador (relay del outbox) usa Inject con el trace_id
// guardado en la fila; el consumidor usa Extract al principio de
// handleDelivery.
package obsamqp

import (
	"context"

	"github.com/ZonaTandas/lib-commons/src/obs"
	amqp "github.com/rabbitmq/amqp091-go"
)

// HeaderTraceID es el header AMQP de correlación.
const HeaderTraceID = "x-trace-id"

// Inject añade el traceId del context a la tabla de headers (creándola si
// hace falta) y la devuelve.
func Inject(ctx context.Context, headers amqp.Table) amqp.Table {
	if headers == nil {
		headers = amqp.Table{}
	}
	if tid := obs.TraceID(ctx); tid != "" {
		headers[HeaderTraceID] = tid
	}
	return headers
}

// Extract devuelve un context (sobre base) con el traceId del mensaje; si el
// mensaje no trae, genera uno nuevo para que el procesado del consumer quede
// igualmente correlacionado.
func Extract(base context.Context, headers amqp.Table) context.Context {
	tid := ""
	if headers != nil {
		if v, ok := headers[HeaderTraceID].(string); ok {
			tid = v
		}
	}
	if tid == "" {
		tid = obs.NewTraceID()
	}
	// Bolsa incluida: obs.Add funciona dentro del consumer igual que en HTTP.
	return obs.WithFieldBag(obs.WithTraceID(base, tid))
}
