# CLAUDE.md — lib-commons

## Resumen del proyecto

Librería Go compartida del ecosistema **ZonaTandas** (SaaS de gestión de tandas/circuitos: microservicios Go + frontend web). Contiene `src/jwt` (JWT HS256 service-to-service) y `src/obs` (observabilidad del evolutivo 2026-07: logs JSON estructurados, traceId extremo a extremo, métricas HTTP). No es un servicio ejecutable: no tiene `main`, ni Docker, ni CI.

- Módulo Go: **`github.com/ZonaTandas/lib-commons`** (renombrado 2026-07-15; antes era `lib-commons` y nadie podía importarlo — ver gotchas).
- El `go.mod` vive en la **raíz del repo** (patrón lib-track-management); los paquetes siguen bajo `src/`.
- Go 1.26. Dependencias: `golang-jwt/jwt/v5`, `joho/godotenv`, `gorilla/mux` (template de ruta en obs), `prometheus/client_golang` (solo obsmetrics), `rabbitmq/amqp091-go` (solo obsamqp).
- Remote: `https://github.com/ZonaTandas/lib-commons.git`.

## Estructura de directorios

```
go.mod              # módulo github.com/ZonaTandas/lib-commons (RAÍZ del repo)
src/
  .env.example      # JWT_SECRET
  jwt/              # Claims / GenerateToken / VerifyToken (HS256, exp 1h fija)
  obs/              # núcleo observabilidad (stdlib + gorilla/mux)
    obs.go          # Init(service) / NewTraceID / TraceID / WithTraceID
    fields.go       # Add(ctx,k,v) + Logger(ctx) — bolsa mutable en ctx
    middleware.go   # Middleware: X-Trace-Id, línea http_request, cuerpos 8KB
                    # enmascarados en escrituras+errores, Flusher/Hijacker,
                    # RouteTemplate(r) (template mux); skip /health /metrics
    detach.go       # Detach(ctx) para goroutines best-effort
    client.go       # NewRequest/Do — clientes salientes con trace + http_out
    mask.go         # MaskJSON: redacta credenciales, parcializa dni/gobId
    obsamqp/        # Inject/Extract del header AMQP x-trace-id
    obsmetrics/     # Middleware + Handler: http_requests_total / duration
```

Los paquetes nuevos se añaden como subdirectorios de `src/`.

## Comandos esenciales

Todo se ejecuta desde la **raíz del repo** (ahí vive el `go.mod`):

```bash
go build ./... && go vet ./... && go test ./...
```

Toolchain local: `~/sdk/go1.26.0/bin` (no está en PATH), `GOFLAGS=-mod=mod`.

## Publicación y consumo

- Los servicios lo consumen versionado: `require github.com/ZonaTandas/lib-commons v0.0.X`.
- Publicar versión: `git tag v0.0.X && git push --tags` (el repo es privado → los Dockerfiles consumidores necesitan `ARG GH_PAT` + `GOPRIVATE=github.com/ZonaTandas/*` + `git config url...insteadOf`, patrón de track-management-service).
- Desarrollo local: cada servicio consumidor tiene un `go.work` (en su `.gitignore`, JAMÁS commitearlo: rompería el build de Docker) con `use (. ../../lib-commons)`.

## Configuración

- `JWT_SECRET` (src/.env.example): obligatoria para jwt; no falla si falta (firma con clave vacía).
- obs (todas con default): `OBS_LOG_LEVEL=info`, `OBS_MAX_BODY_BYTES=8192`, `OBS_CAPTURE_BODIES=writes+errors|errors|off`, `OBS_SAMPLE_PATHS` (CSV de prefijos de ruta que solo loguean errores).

## Gotchas y trampas conocidas

1. **El módulo se renombró** de `lib-commons` a `github.com/ZonaTandas/lib-commons` (2026-07-15, evolutivo observabilidad) y el go.mod se movió de `src/` a la raíz. Los imports internos (tests incluidos) usan la ruta completa `github.com/ZonaTandas/lib-commons/src/...`.
2. **`JWT_SECRET` vacío no produce error**: `GenerateToken`/`VerifyToken` usan `os.Getenv` sin validar.
3. **`VerifyToken` hace type assertions sin comprobar**: un token válido firmado con el mismo secreto pero sin algún claim provoca **panic** (solo afecta a tokens generados fuera de esta librería).
4. La expiración JWT está **hardcodeada a 1 hora**.
5. Mensajes de error de jwt en español: usar `ErrInvalidToken`/`ErrExpiredToken`, nunca el texto.
6. **obs.Middleware solo captura el reqBody que el handler LEE** (TeeReader): si un handler corta antes de leer el body, ese cuerpo no sale en el log.
7. **obs.Detach NO añade cancelación** (context.WithoutCancel): las goroutines dependen de los timeouts de su http.Client.
8. El núcleo obs importa `gorilla/mux` (para el template de ruta): no es 100% stdlib, pero todos los servicios ya usan mux. Copiable como plan B igualmente.
9. `traceId` **jamás** debe ser label de Loki ni de Prometheus (cardinalidad); en Prometheus `route` es siempre el template de mux.
