# Changelog

## v1.4.0 - 2026-10-19

### Incompatible

- `policies/authz/authz.rego` (and the `authz/opa` test fixture) anchors every `data.whitelist` and `data.permissions` pattern at the start of the action: `regex.match("^(?:" + pattern + ")", input.action)`. Before, `GET:/admin/secret?x=GET:/health` matched the whitelisted `GET:/health$` because `middleware.Authorize` evaluates `METHOD:/path?query` and the match was unanchored, so any allowed action smuggled into the query or the path granted access. Patterns that relied on matching a substring must be rewritten with their intended prefix; patterns already starting with `^` are unaffected. The example `permissions.json` now ends `GET:/status` and `GET:/api/panic` with `$`.
- `health.Probe` no longer puts each failing checker's `err.Error()` in the response body (it exposed internal hosts to whoever can reach the probe). Opt back in with `health.WithErrorDetails()`; log failures with the new `health.WithLogger(l)` (`health check failed`, `health.check`, standard error object).
- `middleware.Option` is renamed `middleware.InterceptOption` and `middleware.InterceptorOptions` (exported struct with only unexported fields) is unexported; `WithIntercept`/`WithMessage` return the new type. Every middleware constructor now returns `middleware.Middleware` instead of the spelled-out `func(http.Handler) http.Handler` (same underlying type; assignments compile unchanged).
- `presenter.Renderer` and `presenter.Render` are removed: no implementation or caller existed. Use `presenter.JSON` directly.
- `problem.ContextWithRequestID`, `problem.RequestIDFromContext` and `problem.WithRequestIDFromContext` are removed. The request id in problem+json was always the W3C trace id stored twice in the context by `middleware.TraceContext`; `problem.WithInstance` now reads it from `trace.FromContext` directly (one context value per request instead of two). `problem.WithRequestID(id)` stays for ids that are not the trace id. Migration: replace `problem.ContextWithRequestID(ctx, id)` with `trace.NewContext(ctx, trace.Context{TraceID: id, SpanID: ...})`, or pass `problem.WithRequestID(id)` explicitly.
- `middleware.CORS` panics at construction when `AllowCredentials` is combined with a wildcard origin (`"*"` or an empty `AllowedOrigins`, which means "all"): browsers reject `Access-Control-Allow-Origin: *` on credentialed requests, so the configuration could never work. List the origins or use `AllowOriginFunc`. `middleware.AllowAll` therefore no longer sets `AllowCredentials: true` (same default as rs/cors); opt in with `AllowAll(func(o *CORSOptions) { o.AllowedOrigins = []string{...}; o.AllowCredentials = true })`.
- `presenter.Bind` is now `Bind(w http.ResponseWriter, r *http.Request, v Binder, opts ...BindOption) error`. The body is wrapped in `http.MaxBytesReader` (1 MiB by default, `presenter.WithMaxBodyBytes` per call) and every failure is a `*presenter.BindError` whose `Status` is `413` on overflow and `400` otherwise; the dead `io.Copy(io.Discard)` drain was removed. Migration: add the `w` argument and map `BindError.Status` to your error response.
- `server.New` defaults changed: `ReadTimeout` 15s → 30s, `WriteTimeout` 15s → 60s (safety nets; use `middleware.Timeout` per route), `MaxHeaderBytes` 1 MiB → 64 KiB, plus `MaxHeaderValueCount` 256 and an `HTTP2Config` (`MaxConcurrentStreams` 250, `SendPingTimeout` 30s, `PingTimeout` 15s, `WriteByteTimeout` 15s). Override with `server.With`.
- `logging.Err` captures at most 32 frames with `runtime.Callers`; `error.stack_trace` no longer starts with `goroutine N [running]:`. Alerts matching on that header must be updated.

### Added

- `make bench` and `BenchmarkRecommendedChain` / `BenchmarkMiddlewareEach` in `middleware`: a full request through the documented chain and each middleware in isolation, for `benchstat` comparisons.
- `middleware.Timeout(d)`: per-request deadline in the context; responds 504 problem+json when the handler returns on deadline without having committed a response. No buffering, streaming-safe; handlers that ignore `ctx` are not interrupted.
- `middleware.Shed(maxInFlight)`: bulkhead that rejects requests beyond the limit with 503 problem+json and `Retry-After: 1`.
- `server.WithDrainDelay(d)`: keeps serving for `d` after `OnShutdown` hooks and before `Shutdown`, closing the Kubernetes Endpoints propagation window. Default 0.
- `server.Run` sets `http.Server.ErrorLog` to the structured logger when unset, so `net/http` internal errors are NDJSON.
- `health.WithCheckTimeout(d)`: per-checker deadline in `Probe`.
- `middleware.WrapWriter` implements `http.Flusher` and `io.ReaderFrom` directly (sendfile and `w.(http.Flusher)` keep working through the wrapper); `InterceptStatus` materializes a pending intercepted status on `Flush` and drops stale `Content-Length`/`Content-Encoding`/`ETag` before writing the problem document.

### Fixed

- `health.Probe` no longer hangs the request when a `Checker` ignores its context: the wait is bounded by the request context and `WithCheckTimeout`, and a checker still running past the deadline is reported as `"TIMEOUT"` (new `CheckResult.Status` value) while the others keep their result.
- `middleware.Recover` no longer writes a 500 problem document on top of a response that was already committed through the package's writer wrapper (`RequestLogger`, `Timeout` or `InterceptStatus` earlier in the chain); the panic is still logged.
- `authz/opa.Client.IsAllowed` returns `false, nil` on a result without expressions instead of panicking, matching `Query`.
- `middleware.RequestLogger` emits the access line with `slog.Logger.LogAttrs`: 6 fewer allocations per request (7 → 1 with a disabled logger), identical output.
- `middleware.Authorize` no longer sends the enforcer's error message to the client in the 500 body (it exposed OPA internals) and now logs it through the new `middleware.WithAuthorizeLogger(l)` option, following the same 4xx/5xx policy as `presenter.Error`. The signature gains a variadic `...AuthorizeOption` and returns `Middleware`; existing calls compile unchanged.
- `middleware.InterceptStatus` shares the package's single `ResponseWriter` wrapper instead of its own: `io.Copy(w, file)` behind it reaches the underlying `ReadFrom` again (sendfile), the access line's `http.response.body.size` reports the problem document actually sent, and a second `WriteHeader` after an intercepted status no longer commits a different status ahead of the document. `middleware.WrapWriter` is idempotent: wrapping an already wrapped writer returns the existing wrapper, so stacked middlewares share one status/bytes/committed state (two fewer allocations per request in the recommended chain).
- `authz/opa.New`/`Reload` now reject documents that are not valid JSON (e.g. `NaN`): the in-memory store's own round trip accepted them and stored `NaN` as a `json.Number`. The document is validated once with `util.RoundTrip` and written pre-normalized.
- `go.mod` pins `toolchain go1.27.2` (fixes the HTTP/2 HPACK race crash shipped in Go 1.27.0/1.27.1).

## v1.3.0 - 2026-10-01

### Incompatible

- `go.mod` requires Go 1.27.
- `presenter.JSON`, `presenter.Bind`, `problem.Respond` and `middleware.InterceptStatus` now use `encoding/json/v2`. Wire changes on output: no trailing newline after the body; nil slices encode as `[]` and nil maps as `{}` (previously `null`); `<`, `>`, `&` are no longer escaped; `omitempty` no longer omits `0`, `false` or a nil `[]byte` (use `omitzero` for Go-zero semantics); `[N]byte` arrays encode as base64 strings; a `time.Duration` field without a format tag is an encode error. `presenter.JSON` now marshals in memory first with `json.Deterministic(true)` (map keys sorted): an encode failure yields a 500 with an empty body instead of a 200 with an empty or truncated one. Consumers wanting `null` for nil collections pass `json.FormatNilSliceAsNull(true)` / `json.FormatNilMapAsNull(true)` in their own code.
- `presenter.Bind` is stricter: JSON object names are matched case-sensitively, duplicate names and invalid UTF-8 are rejected, trailing data after the value is rejected, and an empty body is an error that no longer wraps `io.EOF` (map all of these to your 400 path by checking `err != nil`, not `errors.Is(err, io.EOF)`).

### Added

- `authz/opa.WithData(map[string]any)`: seed the OPA data document from memory instead of `DataFiles` (mutually exclusive). In this mode only `.rego` files under `PoliciesPath` are loaded (parsed once at `New`); data files there are ignored. A nil map is an error at `New`, never a silent fallback to files.
- `authz/opa.Client.Reload(ctx, data)`: atomically replace the data document and re-prepare every compiled query; evaluations in flight keep the previous snapshot, and a failed reload (unencodable data, compile error) returns an error and keeps the previous state — it never panics.
- A `Query` prepare failure is no longer permanent for the client's lifetime: it is returned and retried on the next call.

## v1.2.0 - 2026-08-19

Additive release. No consumer needs code changes: the field shows up on upgrade.

### Added

- `logging.KeyEventDurationHuman` (`event.duration_human`): the `logging` handler automatically adds a human-readable mirror to every record carrying `event.duration` as a top-level attr (`slog.Int64` in nanoseconds, or `slog.Duration`), formatted with `time.Duration.String()`. It is a shortcut for reading raw logs: aggregation and alerting still happen on `event.duration`. Emitted on every `middleware.RequestLogger` line with no configuration. Limitation: a duration pre-bound with `With`/`WithAttrs`, or written inside a group, is not mirrored — the same limitation `trace_id`/`span_id` already have. If your service already emits this field through a local handler decorator, remove that code in the same upgrade to avoid a duplicate JSON key.

## v1.1.0 - 2026-08-11

Adoption of the Microservices Logging Standard v1.0. This release contains deliberate breaking changes (private library, consumers migrate in a coordinated way; the `/v2` module path is avoided).

### Incompatible

- Removed `middleware.TraceID`, `middleware.WithHeaderName`, `middleware.WithLogger` (the TraceID variant) and `middleware.TraceIDFromContext`. Migration: `middleware.TraceID(middleware.WithHeaderName("X-Request-ID"))` → `middleware.TraceContext(middleware.WithResponseHeader("X-Request-ID"))`; `middleware.TraceIDFromContext(ctx)` → `trace.FromContext(ctx)`.
- `github.com/google/uuid` is no longer a direct dependency; IDs are now W3C Trace Context (trace_id 128-bit hex, span_id 64-bit hex). The module still appears in `go.mod` as `// indirect` because OPA requires it transitively.
- `RequestLogger` emits `message:"request completed"` with OTel fields (`http.request.method`, `url.path`, `http.response.status_code`, `http.response.body.size`, `client.address`, `event.duration` in nanoseconds); the former `http` group, the `logger` field and the string-formatted duration are gone. Update dashboards and alerts.
- `Recover` emits `panic recovered` with `error.type`/`error.message`/`error.stack_trace` (previously `stacktrace`).
- Log messages renamed in `server` (`server listening`, `server draining`), `presenter` (`json encoding failed`), `problem` (`problem encoding failed`) and `middleware.CORS` (`preflight rejected`, `cors headers skipped`). In `authz/opa.New` the query-preparation failure log (previously `opa query preparation failed`) was removed: the error is already returned wrapped and the caller logs it, which avoids double logging (log-and-rethrow).
- `go.mod` requires Go 1.26.

### Added

- `logging` package: the platform-standard logger (`New`, `WithService`, `WithEnvironment`, `WithLevel`, `Err`, `Key*` constants) — NDJSON, OTel/ECS fields, ISO 8601 UTC timestamps with milliseconds, automatic trace context.
- `trace` package: W3C Trace Context in pure stdlib (`Parse`, `New`, `NewSpanID`, `NewContext`/`FromContext`, `Traceparent`, `Transport`).
- `middleware.TraceContext` with an optional `WithResponseHeader`.
- `docs/logging.md`: catalog of fields and messages.

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
