// Package obs es el núcleo de observabilidad de ZonaTandas (evolutivo
// 2026-07-observabilidad): logs JSON estructurados con slog, traceId
// extremo a extremo (header X-Trace-Id), campos de negocio acumulables en
// el context y captura de cuerpos enmascarados en escrituras y errores.
//
// Diseño: núcleo autocontenido (stdlib + gorilla/mux para el template de
// ruta) para poder copiarlo a un servicio como plan B. Los subpaquetes
// obsamqp (amqp091) y obsmetrics (client_golang) llevan las dependencias
// opcionales.
package obs

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// HeaderTraceID es la cabecera HTTP de correlación. Nace en el BFF de los
// frontends y viaja por HTTP, AMQP (header x-trace-id) y el outbox.
const HeaderTraceID = "X-Trace-Id"

// HeaderInternalAuth es la cabecera de verificación de origen interno
// (evolutivo 2026-07-red-interna-auth): las llamadas entre servicios (y desde
// los BFF web-app/web-panel) la llevan con el INTERNAL_SHARED_SECRET o con un
// token caducable derivado de él (ver internalauth.go). RequireInternalAuth
// la valida en la entrada.
const HeaderInternalAuth = "X-Internal-Auth"

type ctxKeyTraceID struct{}

// Init instala slog.Default como JSONHandler a stdout con el atributo fijo
// service. Nivel vía OBS_LOG_LEVEL (debug|info|warn|error, default info).
func Init(service string) {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("OBS_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler).With("service", service))
}

// NewTraceID genera un UUID v4 con crypto/rand.
func NewTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand no falla en la práctica; si fallara, mejor un id fijo
		// reconocible que un panic en el middleware.
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // versión 4
	b[8] = (b[8] & 0x3f) | 0x80 // variante RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TraceID devuelve el traceId del context ("" si no hay).
func TraceID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyTraceID{}).(string); ok {
		return v
	}
	return ""
}

// WithTraceID devuelve un context con el traceId fijado.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyTraceID{}, id)
}
