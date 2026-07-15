# lib-commons

Librería Go compartida del ecosistema ZonaTandas.

- Módulo: **`github.com/ZonaTandas/lib-commons`** (go.mod en la raíz del repo).
- Paquetes: `src/jwt` (JWT service-to-service) y `src/obs` (observabilidad:
  logs JSON + traceId + métricas; evolutivo 2026-07-observabilidad).

## Consumo desde un servicio

```bash
go get github.com/ZonaTandas/lib-commons@v0.0.1
```

El repo es privado: el Dockerfile del servicio necesita el patrón `GH_PAT` +
`GOPRIVATE=github.com/ZonaTandas/*` (igual que lib-track-management). Para
publicar una versión: `git tag v0.0.X && git push --tags`.

Para desarrollo local sin publicar, cada servicio usa un `go.work` (NO se
commitea; está en .gitignore) que apunta a esta copia local:

```
go 1.26

use (
	.
	../../lib-commons
)
```

## src/obs — observabilidad

```go
import (
    "github.com/ZonaTandas/lib-commons/src/obs"
    "github.com/ZonaTandas/lib-commons/src/obs/obsamqp"
    "github.com/ZonaTandas/lib-commons/src/obs/obsmetrics"
)

func main() {
    obs.Init("mi-servicio") // slog JSON a stdout con service fijo

    router := mux.NewRouter()
    router.Use(obs.Middleware, obsmetrics.Middleware)
    router.Handle("/metrics", internalapi.ServiceToken(obsmetrics.Handler()))
}
```

- `obs.Middleware`: extrae/genera `X-Trace-Id`, lo devuelve en la respuesta,
  loguea la línea de acceso `http_request` con route (template de mux),
  status, duración, campos de negocio y cuerpos (escrituras + errores,
  truncados y enmascarados). Implementa Flusher/Hijacker (SSE OK).
- `obs.Add(ctx, "pnr", pnr)`: acumula campos de negocio que salen en la línea
  de acceso y en todo `obs.Logger(ctx)`.
- `obs.Logger(ctx)`: sustituto de `fmt.Println` — slog con traceId + campos.
- `obs.NewRequest/Do`: clientes HTTP inter-servicio con propagación del trace
  y log `http_out`.
- `obs.Detach(ctx)`: para goroutines best-effort (sobrevive a la cancelación
  de la request y copia traceId/campos). Llamar SIEMPRE antes del `go func`.
- `obsamqp.Inject/Extract`: header AMQP `x-trace-id` (publisher/consumer).
- `obsmetrics`: `http_requests_total{route,method,status}` +
  `http_request_duration_seconds{route,method}`.

Envs (con defaults): `OBS_LOG_LEVEL=info`, `OBS_MAX_BODY_BYTES=8192`,
`OBS_CAPTURE_BODIES=writes+errors|errors|off`, `OBS_SAMPLE_PATHS` (CSV de
prefijos que solo loguean errores: availability, SSE).