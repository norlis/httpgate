# Changelog

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
- **`opa.Client.Data(ctx, path) (any, error)`** — declarative-data
  introspection. Reads `data.<path>` from the loaded bundle (bracket
  notation, so segments that collide with Rego reserved words like
  `not`, `if`, `in` work). Caches the prepared query per path,
  single-prepare under concurrent first-access (`sync.Once`).
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
