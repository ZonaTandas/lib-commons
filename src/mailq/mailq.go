// Package mailq es el ÚNICO cliente de correo transaccional del ecosistema
// (evolutivo 2026-08-preparacion-lanzamiento §1). Publica el email en la cola
// AMQP notifications.mail (durable, con confirms) y, si no hay broker o el
// publish falla, degrada al POST /queue/create histórico de notifications:
// el contrato JSON es el mismo por los dos caminos.
//
// Reglas que viven aquí y NO en los llamantes:
//   - escapado HTML de las variables (las plantillas las inyectan tal cual);
//   - messageId (uuid v4) para idempotencia at-least-once: notifications lo usa
//     como PK de mail_queue y el duplicado es un no-op;
//   - idioma por defecto "es".
package mailq

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"

	"github.com/ZonaTandas/lib-commons/src/obs"
	"github.com/ZonaTandas/lib-commons/src/obs/obsamqp"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Email es un correo transaccional a encolar.
type Email struct {
	To          string
	Template    string // nombre SIN sufijo de idioma (notifications resuelve template_language)
	Language    string // "" = "es"
	Variables   map[string]string
	Attachments []Attachment
	From        string // "" = MAIL_DEFAULT_SENDER de notifications
	MessageID   string // "" = uuid v4 generado; fija uno propio para dedupe entre reintentos
}

// Attachment adjunta un fichero POR REFERENCIA: notifications lo descarga con
// el service token en el momento del envío.
type Attachment struct {
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	ContentType string `json:"contentType,omitempty"`
}

// Message es el contrato JSON compartido por AMQP y por el POST /queue/create.
// Lo decodifica el consumer de notifications-service: mismo tipo en los dos lados.
type Message struct {
	MessageID   string            `json:"messageId,omitempty"`
	ToEmail     string            `json:"toEmail"`
	Template    string            `json:"template"`
	Language    string            `json:"language"`
	Variables   map[string]string `json:"variables"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	FromEmail   string            `json:"fromEmail,omitempty"`
}

// Send encola el email. Devuelve error solo si fallan AMBOS transportes (o la
// validación); los llamantes deciden si loguear o reintentar/requeue.
func Send(ctx context.Context, email Email) error {
	if email.To == "" {
		return errors.New("mailq: destinatario vacío")
	}
	if email.Template == "" {
		return errors.New("mailq: plantilla vacía")
	}
	if email.Language == "" {
		email.Language = "es"
	}
	if email.MessageID == "" {
		email.MessageID = newUUID()
	}

	escaped := make(map[string]string, len(email.Variables))
	for k, v := range email.Variables {
		escaped[k] = html.EscapeString(v)
	}
	body, err := json.Marshal(Message{
		MessageID: email.MessageID, ToEmail: email.To, Template: email.Template,
		Language: email.Language, Variables: escaped,
		Attachments: email.Attachments, FromEmail: email.From,
	})
	if err != nil {
		return fmt.Errorf("mailq: serializar: %w", err)
	}

	if RabbitURL() != "" {
		if err := amqpPublish(body, obsamqp.Inject(ctx, nil)); err == nil {
			return nil
		} else {
			// El fallback HTTP existe justo para esto; el fallo AMQP queda en el log.
			obs.Logger(ctx).Warn(fmt.Sprintf("mailq: publish AMQP falló, degrado a HTTP: %v", err))
		}
	}
	return httpSend(ctx, body)
}

// RabbitURL devuelve la URL del broker; vacía = AMQP desactivado (E2E, local).
func RabbitURL() string { return os.Getenv("RABBITMQ_URL") }

// amqpPublish es un seam para tests; en producción publica con confirms.
var amqpPublish = func(body []byte, headers amqp.Table) error {
	return publishAMQP(body, headers)
}

// newUUID genera un uuid v4 con crypto/rand (sin dependencia externa).
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // rand.Read no falla en la práctica; si falla, nada es de fiar
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
