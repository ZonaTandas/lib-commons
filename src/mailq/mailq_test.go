package mailq

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

// restore deja los seams de transporte como estaban al acabar cada test.
func restore(t *testing.T) {
	t.Helper()
	prevPublish := amqpPublish
	t.Cleanup(func() { amqpPublish = prevPublish })
}

func TestSendValidatesRecipientAndTemplate(t *testing.T) {
	restore(t)
	if err := Send(t.Context(), Email{Template: "welcome_mail"}); err == nil {
		t.Fatal("sin destinatario debería fallar")
	}
	if err := Send(t.Context(), Email{To: "a@b.com"}); err == nil {
		t.Fatal("sin plantilla debería fallar")
	}
}

func TestSendFallsBackToHTTPWithoutBroker(t *testing.T) {
	restore(t)
	t.Setenv("RABBITMQ_URL", "")
	t.Setenv("AUTH_SERVICE_TOKEN", "secreto")

	var got Message
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/queue/create" {
			t.Errorf("path inesperado %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("body ilegible: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	t.Setenv("NOTIFICATIONS_SERVICE_URL", srv.URL)

	err := Send(t.Context(), Email{
		To:       "user@example.com",
		Template: "booking_confirmed",
		Variables: map[string]string{
			"pnr":  "BCD234",
			"name": "Ana <script>",
		},
		Attachments: []Attachment{{Filename: "f.pdf", URL: "https://internal/x.pdf", ContentType: "application/pdf"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if auth != "Bearer secreto" {
		t.Fatalf("Authorization = %q", auth)
	}
	if got.ToEmail != "user@example.com" || got.Template != "booking_confirmed" {
		t.Fatalf("Message inesperado: %+v", got)
	}
	if got.Language != "es" {
		t.Fatalf("language default debería ser es, fue %q", got.Language)
	}
	// El escapado HTML vive aquí, no en los llamantes (regla del rediseño §1).
	if got.Variables["name"] != "Ana &lt;script&gt;" {
		t.Fatalf("variable sin escapar: %q", got.Variables["name"])
	}
	if len(got.Attachments) != 1 || got.Attachments[0].URL != "https://internal/x.pdf" {
		t.Fatalf("attachments perdidos: %+v", got.Attachments)
	}
	if !uuidRe.MatchString(got.MessageID) {
		t.Fatalf("messageId no es uuid: %q", got.MessageID)
	}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestSendKeepsCallerMessageID(t *testing.T) {
	restore(t)
	t.Setenv("RABBITMQ_URL", "")
	var got Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	t.Setenv("NOTIFICATIONS_SERVICE_URL", srv.URL)

	err := Send(t.Context(), Email{To: "a@b.com", Template: "t", MessageID: "id-propio"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.MessageID != "id-propio" {
		t.Fatalf("messageId pisado: %q", got.MessageID)
	}
}

func TestSendHTTPErrorSurfaces(t *testing.T) {
	restore(t)
	t.Setenv("RABBITMQ_URL", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("NOTIFICATIONS_SERVICE_URL", srv.URL)

	if err := Send(t.Context(), Email{To: "a@b.com", Template: "t"}); err == nil {
		t.Fatal("un 500 del fallback debería devolver error")
	}
}

func TestSendPrefersAMQPAndSkipsHTTP(t *testing.T) {
	restore(t)
	t.Setenv("RABBITMQ_URL", "amqp://localhost:5672")
	httpCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	t.Setenv("NOTIFICATIONS_SERVICE_URL", srv.URL)

	var published []byte
	amqpPublish = func(body []byte, headers amqp.Table) error {
		published = body
		return nil
	}

	if err := Send(t.Context(), Email{To: "a@b.com", Template: "t"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if published == nil {
		t.Fatal("no publicó por AMQP")
	}
	if httpCalled {
		t.Fatal("con AMQP OK no debe tocar HTTP")
	}
	var got Message
	if err := json.Unmarshal(published, &got); err != nil || got.ToEmail != "a@b.com" {
		t.Fatalf("mensaje AMQP ilegible: %v %+v", err, got)
	}
}

func TestSendFallsBackToHTTPWhenAMQPFails(t *testing.T) {
	restore(t)
	t.Setenv("RABBITMQ_URL", "amqp://localhost:5672")
	httpCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	t.Setenv("NOTIFICATIONS_SERVICE_URL", srv.URL)

	amqpPublish = func([]byte, amqp.Table) error { return errors.New("broker caído") }

	if err := Send(t.Context(), Email{To: "a@b.com", Template: "t"}); err != nil {
		t.Fatalf("con fallback sano no debe fallar: %v", err)
	}
	if !httpCalled {
		t.Fatal("el fallo AMQP debe degradar a HTTP")
	}
}
