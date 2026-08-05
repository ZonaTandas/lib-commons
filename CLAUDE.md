# CLAUDE.md — lib-commons

## Resumen del proyecto

Librería Go compartida del ecosistema **ZonaTandas** (SaaS de gestión de tandas/circuitos: microservicios Go + frontales Next.js). Contiene `src/obs` (observabilidad del evolutivo 2026-07: logs JSON estructurados, traceId extremo a extremo, métricas HTTP) y la verificación de origen interno `X-Internal-Auth` (evolutivo 2026-07-red-interna-auth). No es un servicio ejecutable: no tiene Docker ni deploy; el único binario es el CLI `cmd/internal-token`.

- Módulo Go: **`github.com/ZonaTandas/lib-commons`** (renombrado 2026-07-15; antes era `lib-commons` y nadie podía importarlo — ver gotchas).
- El `go.mod` vive en la **raíz del repo** (patrón lib-track-management); los paquetes bajo `src/`.
- Go 1.26, `toolchain go1.26.5` (bundle B2 del saneamiento: 1.26.0 arrastra ~14 vulns de stdlib alcanzables). Dependencias: `gorilla/mux` (template de ruta en obs), `prometheus/client_golang` (solo obsmetrics), `rabbitmq/amqp091-go` (solo obsamqp).
- Remote: `https://github.com/ZonaTandas/lib-commons.git`.
- **La consumen los 23 servicios Go** de la plataforma.

## Estructura de directorios

```
go.mod              # módulo github.com/ZonaTandas/lib-commons (RAÍZ del repo)
.github/workflows/tests.yaml  # go vet + go test con coverage en cada push
.env.sample         # inventario de env vars que LEE la lib (no carga .env)
cmd/internal-token/ # CLI: emite tokens caducables de X-Internal-Auth
src/authn/          # verificación de identidad entre servicios
  verifysession.go  # VerifySession: sesión de usuario contra oauth /verify,
                    # con traceId + X-Internal-Auth; errores tipados
                    # (ErrNoSession/ErrInvalidSession/ErrUpstream/ErrNotConfigured)
  servicetoken.go   # CheckServiceToken: AUTH_SERVICE_TOKEN fail-closed,
                    # leído por request, comparado en tiempo constante
src/mailq/          # cliente ÚNICO de correo transaccional (10 servicios lo usan)
  mailq.go          # Send: publica en la cola AMQP notifications.mail, con
                    # fallback a POST /queue/create; messageId uuid v4 e
                    # idioma por defecto. Las variables viajan CRUDAS: el
                    # escapado HTML es del renderizador (ver gotcha 13)
  http.go           # transporte de fallback;  topology.go: exchange/colas
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
10. **`authn.VerifySession` nunca lleva la URL de oauth dentro**: se pasa por parámetro (env del servicio, con DNS de clúster). El hallazgo `profiles-01` fue exactamente eso: `https://oauth.service.zonatandas.es` hardcodeado, tráfico interno saliendo al ingress.
11. **`authn.CheckServiceToken` con `AUTH_SERVICE_TOKEN` vacío devuelve `(false,false)`**: el llamante debe responder **503**, no 401 — es misconfig nuestra, no credencial mala del cliente. Colapsar ambos en 401 esconde el despliegue roto.
12. Ramas inalcanzables conocidas (quedan sin cubrir, es esperado): el `"[error al enmascarar]"` de MaskJSON, la redacción por clave heredada en el caso escalar de maskValue, el fallback de NewTraceID y los `os.Exit` del CLI.
13. **`mailq` NO escapa las variables — y no debe volver a hacerlo.** El escapado HTML pertenece a quien renderiza la plantilla (`mailer.replacePlaceholders` de notifications-service): es el único que sabe que el cuerpo va en `text/html` y el único con la lista de excepciones `htmlSafeKeys`. Cuando `mailq` también escapaba, todo se escapaba dos veces: el enlace de verificación de email acababa con `&amp;amp;` en el `href`, el navegador pedía `?email=…&amp;code=X` y el segundo parámetro pasaba a llamarse `amp;code`, así que la página nunca recibía el código. Además la cola se puede llenar por HTTP directo sin pasar por este paquete, así que escapar aquí tampoco cubriría ese camino. Regresión cubierta por `TestSendNoAlteraLasURLsDeLasVariables`.
