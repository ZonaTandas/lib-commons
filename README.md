# lib-commons

Librería Go compartida del ecosistema ZonaTandas. Contiene el núcleo de
observabilidad (`src/obs`: logs JSON + traceId extremo a extremo + captura de
cuerpos enmascarados), la verificación de origen interno `X-Internal-Auth`
(evolutivo 2026-07-red-interna-auth), y sus subpaquetes opcionales de métricas
(`obsmetrics`) y AMQP (`obsamqp`). No es un servicio: no se despliega.

- Módulo: **`github.com/ZonaTandas/lib-commons`** (go.mod en la raíz del repo).
- **La usan los 23 servicios Go** de la plataforma (obs/obsmetrics en todos;
  obsamqp en los que hablan RabbitMQ: booking-service, booking-queue-service,
  booking-workers-service, boxes-service, payment-core-service,
  payment-stripe-service).

## Paquetes

| Paquete | Qué aporta |
|---------|------------|
| `src/obs` | `Init` (slog JSON), `Middleware` (X-Trace-Id + línea `http_request` + cuerpos ≤8KB enmascarados), `Logger(ctx)`/`Add(ctx,k,v)` (campos de negocio), `NewRequest`/`Do` (clientes salientes con trace + `http_out` + X-Internal-Auth), `Detach` (goroutines best-effort), `MaskJSON`, `RequireInternalAuth`/`ValidateInternalAuth`/`NewInternalAuthToken` |
| `src/obs/obsmetrics` | `http_requests_total{route,method,status}`, `http_request_duration_seconds{route,method}`, `internal_auth_requests_total{result,route}`; `Handler()`/`TokenHandler()` para `/metrics` |
| `src/obs/obsamqp` | `Inject`/`Extract` del header AMQP `x-trace-id` |
| `cmd/internal-token` | CLI que emite tokens caducables de X-Internal-Auth |

## Uso en un servicio

```go
import (
    "github.com/ZonaTandas/lib-commons/src/obs"
    "github.com/ZonaTandas/lib-commons/src/obs/obsmetrics"
)

func main() {
    obs.Init("mi-servicio")
    router := mux.NewRouter()
    router.Use(obs.Middleware, obs.RequireInternalAuth, obsmetrics.Middleware)
    router.Handle("/metrics", obsmetrics.TokenHandler())
}
```

- `obs.Middleware`: extrae/genera `X-Trace-Id`, lo devuelve en la respuesta,
  loguea `http_request` con route (template de mux), status, duración, campos
  de negocio y cuerpos (escrituras + errores, truncados y enmascarados).
  Implementa Flusher/Hijacker (SSE OK). Salta `/health` y `/metrics`.
- `obs.RequireInternalAuth`: valida `X-Internal-Auth` (secreto compartido o
  token caducable `v1.<subject>.<exp>.<hmac>`). Log-only por defecto;
  `INTERNAL_AUTH_ENFORCE=true` → 403 (fail-closed si no hay secreto).
  Exenciones: `/health`, `/metrics` y los prefijos de
  `INTERNAL_AUTH_EXEMPT_PATHS`.
- `obs.NewRequest`/`obs.Do`: clientes HTTP inter-servicio; propagan el trace,
  añaden `X-Internal-Auth` y loguean `http_out`. **Solo para llamadas a
  servicios internos**: adjuntan el secreto a cualquier destino (no usar
  contra APIs de terceros).
- `obs.Add(ctx, "pnr", pnr)` + `obs.Logger(ctx)`: campos de negocio y logger
  con traceId; sustituto de `fmt.Println`.
- `obs.Detach(ctx)`: para goroutines best-effort; llamar SIEMPRE antes del
  `go func`. No añade cancelación (dependen del timeout de su http.Client).
- `obsamqp.Inject/Extract`: correlación por RabbitMQ (publisher/consumer).

## Token de acceso para desarrollo (Postman/curl)

Los servicios con enforce activo exigen `X-Internal-Auth`. Sin repartir el
secreto, emite un token caducable:

```bash
INTERNAL_SHARED_SECRET=... go run ./cmd/internal-token -subject dev-<nombre> -ttl 2h
# → poner la salida en la cabecera X-Internal-Auth
```

Cualquier servicio lo valida offline (HMAC-SHA256 + expiración); el subject
sale en logs y métricas para auditoría.

## Variables de entorno

Ver `.env.sample`. Todas las lee la librería en runtime desde el proceso del
servicio que la importa (la lib no carga ficheros .env).

## Consumo y publicación

```bash
go get github.com/ZonaTandas/lib-commons@v0.1.0
```

El repo es privado: el Dockerfile del servicio necesita `ARG GH_PAT` +
`GOPRIVATE=github.com/ZonaTandas/*` + `git config url...insteadOf` (patrón de
track-management-service). Publicar versión: `git tag v0.1.X && git push --tags`.

Para desarrollo local sin publicar, cada servicio usa un `go.work` (NO se
commitea; está en su .gitignore):

```
go 1.26

use (
	.
	../../lib-commons
)
```

## Tests

```bash
go build ./... && go vet ./... && go test ./... -cover
```

Coverage actual ≥95% (evolutivo 2026-07-revision-y-testing); corren en cada
push vía `.github/workflows/tests.yaml`.
