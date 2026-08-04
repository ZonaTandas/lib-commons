package mailq

import (
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Nombres de topología. DEBEN coincidir verbatim con el consumer de
// notifications-service y con infrastructure/rabbitmq/configmaps.yaml (regla S10:
// cambiar topología = cambiarla en los tres sitios a la vez).
const (
	ExchangeMail = "notifications.mail" // direct
	ExchangeDLX  = "notifications.dlx"  // direct: plantilla inexistente / payload ilegible
	QueueMail    = "notifications.mail"
	RoutingMail  = "mail"
)

var (
	connMu sync.Mutex
	conn   *amqp.Connection
)

func connection() (*amqp.Connection, error) {
	connMu.Lock()
	defer connMu.Unlock()
	if conn != nil && !conn.IsClosed() {
		return conn, nil
	}
	c, err := amqp.Dial(RabbitURL())
	if err != nil {
		return nil, err
	}
	ch, err := c.Channel()
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	if err := DeclareTopology(ch); err != nil {
		_ = ch.Close()
		_ = c.Close()
		return nil, err
	}
	_ = ch.Close()
	conn = c
	return conn, nil
}

// DeclareTopology declara exchange, cola y DLQ idempotentemente, para poder
// publicar aunque notifications-service no haya arrancado aún. Exportada porque
// el consumer de notifications declara EXACTAMENTE lo mismo.
func DeclareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(ExchangeMail, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.ExchangeDeclare(ExchangeDLX, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(QueueMail+".dlq", true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(QueueMail+".dlq", QueueMail, ExchangeDLX, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(QueueMail, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    ExchangeDLX,
		"x-dead-letter-routing-key": QueueMail,
	}); err != nil {
		return err
	}
	return ch.QueueBind(QueueMail, RoutingMail, ExchangeMail, false, nil)
}

// publishAMQP publica persistente con publisher confirms (patrón
// booking-service/messaging): una conexión caída aflora como error — y Send
// degrada a HTTP — en vez de perderse en silencio.
func publishAMQP(body []byte, headers amqp.Table) error {
	c, err := connection()
	if err != nil {
		return err
	}
	ch, err := c.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	if err := ch.Confirm(false); err != nil {
		return err
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	if err := ch.Publish(ExchangeMail, RoutingMail, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Headers:      headers, // x-trace-id
		Body:         body,
	}); err != nil {
		return err
	}
	select {
	case c := <-confirms:
		if !c.Ack {
			return fmt.Errorf("mailq: publish nacked")
		}
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("mailq: publish sin confirmar a tiempo")
	}
}
