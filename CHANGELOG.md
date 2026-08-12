# Changelog

## v1.1.0 - 2026-08-11

Adopción del Estándar de Logging para Microservicios v1.0. Esta versión contiene cambios incompatibles deliberados (librería privada, consumidores migran coordinados; se evita el module path /v2).

### Incompatible

- Eliminados `middleware.TraceID`, `middleware.WithHeaderName`, `middleware.WithLogger` (variante de TraceID) y `middleware.TraceIDFromContext`. Migración: `middleware.TraceID(middleware.WithHeaderName("X-Request-ID"))` → `middleware.TraceContext(middleware.WithResponseHeader("X-Request-ID"))`; `middleware.TraceIDFromContext(ctx)` → `trace.FromContext(ctx)`.
- `github.com/google/uuid` dejó de ser dependencia directa; los IDs ahora son W3C Trace Context (trace_id 128-bit hex, span_id 64-bit hex). El módulo sigue apareciendo en `go.mod` como `// indirect` porque OPA lo requiere transitivamente.
- `RequestLogger` emite `message:"request completed"` con campos OTel (`http.request.method`, `url.path`, `http.response.status_code`, `http.response.body.size`, `client.address`, `event.duration` en nanosegundos); desaparecen el grupo `http` anterior, el campo `logger` y la duración como string. Actualizar dashboards y alertas.
- `Recover` emite `panic recovered` con `error.type`/`error.message`/`error.stack_trace` (antes `stacktrace`).
- Mensajes de log renombrados en `server` (`server listening`, `server draining`), `presenter` (`json encoding failed`), `problem` (`problem encoding failed`) y `middleware.CORS` (`preflight rejected`, `cors headers skipped`). En `authz/opa.New` se eliminó el log de fallo de preparación de query (antes `opa query preparation failed`): el error ya se retorna envuelto y el caller lo loguea, así se evita el doble logging (log-and-rethrow).
- `go.mod` requiere Go 1.26.

### Added

- Paquete `logging`: logger estándar de la plataforma (`New`, `WithService`, `WithEnvironment`, `WithLevel`, `Err`, constantes `Key*`) — NDJSON, campos OTel/ECS, timestamp ISO 8601 UTC ms, trace context automático.
- Paquete `trace`: W3C Trace Context en stdlib puro (`Parse`, `New`, `NewSpanID`, `NewContext`/`FromContext`, `Traceparent`, `Transport`).
- `middleware.TraceContext` con `WithResponseHeader` opcional.
- `docs/logging.md`: catálogo de campos y mensajes.

## v1.0.0 — 2026-05-28

First stable release. **Everything below is a breaking change vs. the
pre-v1 codebase.** No compatibility shims are provided; consumers
migrate once.

### Import path moves

| Before | After |
|---|---|
| `pkg/adapter/apidriven/middleware` | `middleware` |
| `pkg/adapter/apidriven/presenters` | `presenter` |
| `pkg/adapter/opa` | `authz/opa` |
| `pkg/application/health` | `health` |
| `pkg/domain` | `authz` |
| `pkg/port` | **removed** — interfaces moved to consumers |
| `pkg/kit/problem` | `problem` |

### Identifier renames

| Before | After |
|---|---|
| `middleware.Chain(mws...) Middleware` (closure) | `middleware.New(mws...) Chain` (struct) + `Then`, `ThenFunc`, `Append`, `Extend` (alice pattern) |
| `middleware.TraceId`, `middleware.TraceIdFromContext`, `traceIdConfig`, `TraceIdOption` | `middleware.TraceID`, `middleware.TraceIDFromContext`, `traceIDConfig`, `TraceIDOption` |
| `middleware.RequestID`, `middleware.GetRequestID` aliases | **removed** |
| `middleware.AuthorizationMiddleware` | `middleware.Authorize` |
| `middleware.APIErrorMiddleware` | `middleware.InterceptStatus` |
| `middleware.WithCustomMessage` | `middleware.WithMessage` |
| `middleware.ErrorMessage` struct | **removed** |
| `middleware.NewWrapResponseWriter(w, proto)` | `middleware.WrapWriter(w)` |
| `middleware.Cors`, `middleware.CorsOptions`, `middleware.NewCors` | `middleware.CORS`, `middleware.CORSOptions`, `middleware.CORS` |
| `middleware.AllowAll(log)` | `middleware.AllowAll(opts ...func(*CORSOptions))` |
| `middleware.Recover(log, render)` | `middleware.Recover(log)` |
| `presenters.Presenters` (interface) | **removed** |
| `presenters.NewPresenters` | **removed** — use free functions |
| `port.PolicyEnforcer` | `authz.Enforcer` |
| `port.Checker` (`Check() error`) | `health.Checker` (`Check(ctx) error`) |
| `domain.PolicyInput` | `authz.Input` |
| `opa.SdkClient` | `opa.Client` |
| `opa.NewOpaSdkClientFromConfig(ctx, cfg, log)` | `opa.New(ctx, cfg, opa.WithLogger(log))` |
| `problem.ProblemDetail` | `problem.Detail` |
| `problem.RespondError` | `problem.Respond` |
| `problem.Extension` (embedded struct) | flattened into `Detail`; field `RequestId` → `RequestID` |

### New

- **`middleware.Chain`** — immutable struct (alice pattern). Methods:
  `New(mws...) Chain`, `Then(h)`, `ThenFunc(fn)`, `Append(mws...)`,
  `Extend(other)`. Defensive copy in `New`; nil-handler defaults to
  `http.DefaultServeMux`.
- **`middleware.CSRFProtect(opts ...CSRFOption) Middleware`** — wraps
  Go 1.25's `net/http.CrossOriginProtection`. Blocks cross-origin
  state-changing requests via `Sec-Fetch-Site` + `Origin`/`Host`
  comparison. Complements CORS; does not replace it. Options:
  `WithTrustedOrigin`, `WithBypassPattern`, `WithDenyHandler`. Panics at
  boot on malformed trusted origins.
- **`middleware.WrapResponseWriter.BytesWritten() int`** — useful for
  request logging.
- **`opa.Client.Query(ctx, query string, in authz.Input) (any, error)`** —
  evaluates a developer-supplied constant Rego query against `in` via
  `rego.EvalInput`. Prepared queries are cached per query string,
  single-prepare under concurrent first-access (`sync.Once`). An undefined
  query yields `(nil, nil)`. For static reads pass a zero `Input`
  (`Query(ctx, "data.roles", authz.Input{})`). Replaces the earlier
  injection-prone `Data(ctx, path)` (which built the query from caller path
  segments).
- **`opa.Client.Permissions(ctx, in) ([]string, error)`** and
  **`opa.Client.AllowedResources(ctx, in) ([]string, error)`** — capability
  hints for frontends: permission names and flattened regex route patterns
  granted to `in`'s roles (rules `data.authz.permissions` /
  `data.authz.allowed_resources`). Roles must come from the caller's
  authenticated context; these are UI hints, not a security boundary
  (`IsAllowed` still enforces every request).
- **`problem.WithRequestID(id string) Option`** — set RequestID directly
  without passing `*http.Request`.

### Log shape changes

- **`middleware.RequestLogger`** now emits HTTP attributes grouped under
  the `http` key (via `slog.Group("http", ...)`). Consumers ingesting
  these logs must adapt their parsers: fields previously at the top
  level (`status`, `duration`, `uri`, `remoteAddr`) now live under
  `http.status`, `http.duration`, etc. New fields `http.method` and
  `http.bytes` were added.

### Removed extension points

- **`middleware.Recover` no longer accepts a `render` callback**. The
  previous signature `Recover(log, render presenter.Presenters)` is
  gone; the new signature `Recover(log *slog.Logger)` always renders the
  recovered-panic body via `presenter.Error(...)` with `WithStatus(500)`.
  Consumers who customized the response via `render` should now wrap a
  custom middleware **before** `Recover` in the chain. There is no
  built-in replacement for arbitrary customization of the 500 body.

### Security and safety

- **`middleware.CSRFProtect` panics at construction time** when any
  trusted origin string is malformed. Misconfigured security is treated
  as a programming error, not a runtime failure — your service will
  refuse to start, by design.

### Behavioral changes

- `health.Checker.Check` takes `context.Context`. Implementers must
  update their signatures.
- `health.CheckResult.Duration` and `health.Status.Uptime()` are now
  `time.Duration` (JSON: nanoseconds, not human-readable string).
- `problem.WithInstance` reads `r.URL.Path` and the `X-Request-Id`
  header.
- Logger type is `*slog.Logger` everywhere
  (`middleware.WithLogger`, `middleware.RequestLogger`,
  `middleware.Recover`, `middleware.CORSOptions.Logger`,
  `presenter.WithLogger`, `opa.WithLogger`).
- Go minimum version is `1.26`. `go.mod` declares `go 1.26` with
  `toolchain go1.26.3` for reproducible builds. CSRFProtect uses
  `net/http.CrossOriginProtection` (added in Go 1.25), so 1.25 would
  technically suffice for the library code, but the example example uses
  Go 1.26 idioms throughout and the toolchain pin is what we ship.

### Dependencies dropped

- `go.uber.org/fx` (and `go.uber.org/dig`)
- `go.uber.org/zap` (and `go.uber.org/multierr`)

Direct dependencies after v1.0.0:

- `github.com/google/uuid`
- `github.com/open-policy-agent/opa`

### Example

- `example/` → `examples/basic/`. No fx, no zap. Single
  `main.go` with `signal.NotifyContext(os.Interrupt, syscall.SIGTERM)`
  for graceful shutdown via `srv.Shutdown` (10s timeout). `extractRoles`
  is functional (reads `X-Roles` CSV header) instead of returning empty
  roles. Demonstrates the recommended `CORS` + `CSRFProtect` +
  `Authorize` stack.

### Tooling

- `tools/` refreshed: `golangci-lint v2`, `gofumpt`, `govulncheck`,
  `staticcheck`, `goimports`, `shfmt`, `dlv`, `gopls`, `gotestsum`.
- `Makefile` reworked: `make vulncheck`, `make modernize` targets;
  removed dead `LDFLAGS` pointing to non-existent paths.
