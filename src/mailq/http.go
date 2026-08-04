package mailq

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ZonaTandas/lib-commons/src/obs"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

// NotificationsURL devuelve la base de notifications-service para el fallback HTTP.
func NotificationsURL() string {
	if v := os.Getenv("NOTIFICATIONS_SERVICE_URL"); v != "" {
		return v
	}
	return "https://notifications.service.zonatandas.es"
}

// httpSend entrega el mismo JSON por el POST /queue/create histórico.
// obs.NewRequest añade X-Trace-Id y X-Internal-Auth.
func httpSend(ctx context.Context, body []byte) error {
	req, err := obs.NewRequest(ctx, http.MethodPost, NotificationsURL()+"/queue/create", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mailq: construir request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("AUTH_SERVICE_TOKEN"))

	resp, err := obs.Do(ctx, httpClient, req)
	if err != nil {
		return fmt.Errorf("mailq: POST /queue/create: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("mailq: /queue/create respondió %d", resp.StatusCode)
	}
	return nil
}
