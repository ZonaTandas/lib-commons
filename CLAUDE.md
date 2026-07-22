# CLAUDE.md — lib-commons

## Resumen del proyecto

Librería Go compartida del ecosistema **ZonaTandas** (SaaS de gestión de tandas/circuitos: microservicios Go + frontales Next.js). Contiene `src/obs` (observabilidad del evolutivo 2026-07: logs JSON estructurados, traceId extremo a extremo, métricas HTTP) y la verificación de origen interno `X-Internal-Auth` (evolutivo 2026-07-red-interna-auth). No es un servicio ejecutable: no tiene Docker ni deploy; el único binario es el CLI `cmd/internal-token`.

- Módulo Go: **`github.com/ZonaTandas/lib-commons`** (renombrado 2026-07-15; antes era `lib-commons` y nadie podía importarlo — ver gotchas).
- El `go.mod` vive en la **raíz del repo** (patrón lib-track-management); los paquetes bajo `src/`.
- Go 1.26. Dependencias: `gorilla/mux` (template de ruta en obs), `prometheus/client_golang` (solo obsmetrics), `rabbitmq/amqp091-go` (solo obsamqp).
- Remote: `https://github.com/ZonaTandas/lib-commons.git`.
- **La consumen los 23 servicios Go** de la plataforma.

## Estructura de directorios

```
go.mod              # módulo github.com/ZonaTandas/lib-commons (RAÍZ del repo)
.github/workflows/tests.yaml  # go vet + go test con coverage en cada push
.env.sample         # inventario de env vars que LEE la lib (no carga .env)
cmd/internal-token/ # CLI: emite tokens caducables de X-Internal-Auth
src/obs/            # núcleo observabilidad (stdlib + gorilla/mux)
  obs.go            # Init(service) / NewTraceID / TraceID / WithTraceID
  fields.go         # Add(ctx,k,v) + Logger(ctx) — bolsa mutable en ctx
  middleware.go     # Middleware: X-Trace-Id, línea http_request, cuerpos 8KB
                    # enmascarados en escrituras+errores, Flusher/Hijacker,
                    # RouteTemplate(r); skip /health /metrics
  internalauth.go   # RequireInternalAuth / ValidateInternalAuth /
                    # NewInternalAuthToken / SetInternalAuth (X-Internal-Auth)
  detach.go         # Detach(ctx) para goroutines best-effort
  client.go         # NewRequest/Do — clientes salientes con trace + http_out
  mask.go           # MaskJSON: redacta credenciales, parcializa dni/gobId
  obsamqp/          # Inject/Extract del header AMQP x-trace-id
  obsmetrics/       # Middleware + Handler/TokenHandler: http_requests_total /
                    # duration + internal_auth_requests_total (observer en init)
```

Los paquetes nuevos se añaden como subdirectorios de `src/`.

## Comandos esenciales

Todo desde la **raíz del repo** (ahí vive el `go.mod`):

```bash
go build ./... && go vet ./... && go test ./... -cover
```

Toolchain local: `~/sdk/go1.26.0/bin` (no está en PATH), `GOFLAGS=-mod=mod`.
Coverage objetivo ≥95% (evolutivo 2026-07-revision-y-testing); CI en `.github/workflows/tests.yaml` (informativo, sin gate aún).

## Publicación y consumo

- Los servicios lo consumen versionado: `require github.com/ZonaTandas/lib-commons v0.1.X`.
- Publicar versión: `git tag v0.1.X && git push --tags` (repo privado → los Dockerfiles consumidores necesitan `ARG GH_PAT` + `GOPRIVATE=github.com/ZonaTandas/*` + `git config url...insteadOf`, patrón de track-management-service).
- Desarrollo local: cada servicio consumidor tiene un `go.work` (en su `.gitignore`, JAMÁS commitearlo: rompería el build de Docker) con `use (. ../../lib-commons)`.

## Configuración

Ver `.env.sample` (inventario completo comentado). La lib no carga ficheros .env: lee `OBS_LOG_LEVEL`, `OBS_MAX_BODY_BYTES`, `OBS_CAPTURE_BODIES`, `OBS_SAMPLE_PATHS`, `INTERNAL_SHARED_SECRET`, `INTERNAL_AUTH_ENFORCE`, `INTERNAL_AUTH_EXEMPT_PATHS` y (obsmetrics.TokenHandler) `AUTH_SERVICE_TOKEN` del entorno del servicio importador.

## Gotchas y trampas conocidas

1. **El módulo se renombró** de `lib-commons` a `github.com/ZonaTandas/lib-commons` (2026-07-15) y el go.mod se movió de `src/` a la raíz. Los imports internos (tests incluidos) usan la ruta completa `github.com/ZonaTandas/lib-commons/src/...`.
2. **El paquete `src/jwt` ya no existe** (se eliminó con el evolutivo de observabilidad); si algún doc o servicio lo referencia, está desactualizado.
3. **obs.Middleware solo captura el reqBody que el handler LEE** (TeeReader): si un handler corta antes de leer el body, ese cuerpo no sale en el log.
4. **obs.Detach NO añade cancelación** (context.WithoutCancel): las goroutines dependen de los timeouts de su http.Client.
5. **obs.NewRequest/Do/SetInternalAuth adjuntan INTERNAL_SHARED_SECRET a CUALQUIER destino** sin mirar el host: no usarlos contra APIs de terceros (hallazgo libcommons-01 del evolutivo revision-y-testing).
6. **MaskJSON pasa por float64**: enteros >2^53 se corrompen en los logs (hallazgo libcommons-03; test known-failure en mask_extra_test.go).
7. `RequireInternalAuth` en enforce sin secreto configurado devuelve 403 (fail-closed, intencional). Las exenciones (`INTERNAL_AUTH_EXEMPT_PATHS`) son por PREFIJO: `/contents` también exime `/contents-admin`.
8. El núcleo obs importa `gorilla/mux` (para el template de ruta): no es 100% stdlib, pero todos los servicios ya usan mux. Copiable como plan B igualmente.
9. `traceId` **jamás** debe ser label de Loki ni de Prometheus (cardinalidad); en Prometheus `route` es siempre el template de mux.
10. Ramas inalcanzables conocidas (quedan sin cubrir, es esperado): el `"[error al enmascarar]"` de MaskJSON, la redacción por clave heredada en el caso escalar de maskValue, el fallback de NewTraceID y los `os.Exit` del CLI.
