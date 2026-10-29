# httpgate

A small, idiomatic Go library of `net/http` building blocks: middlewares (TraceContext for W3C Trace Context, logger, recover, CORS, CSRF protection, OPA-based authorization, status interceptor), JSON / problem+json response helpers, RFC 9457 problem details, and liveness/readiness probes.

Built on the standard library plus `log/slog` for logging.

## Install

```bash
go get github.com/norlis/httpgate@v1.4.0
```

Requires Go 1.26+.

## Quick start

```go
package main

import (
	"net/http"
	"os"

	"github.com/norlis/httpgate/logging"
	"github.com/norlis/httpgate/middleware"
	"github.com/norlis/httpgate/presenter"
)

func main() {
	logger := logging.New(
		os.Stdout,
		logging.WithService("hello-service", "1.0.0"),
		logging.WithEnvironment("production"),
	)

	chain := middleware.New(
		middleware.TraceContext(),
		middleware.RequestLogger(logger),
		middleware.Recover(logger),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		presenter.JSON(w, r, map[string]string{"hi": "there"})
	})

	_ = http.ListenAndServe(":8080", chain.Then(mux))
}
```

See [`examples/basic/`](examples/basic/) for a full setup with OPA-based
authorization, CSRF protection, graceful shutdown, and a working roles
extractor.

## Recommended chain

Order matters: each middleware relies on the ones before it, and the two protection layers (`Shed`, `Timeout`) sit after logging and recovery so rejections are logged and a panic frees its slot.

```go
chain := middleware.New(
	middleware.TraceContext(middleware.WithResponseHeader("X-Request-ID")),
	middleware.InterceptStatus(middleware.WithIntercept(http.StatusNotFound, http.StatusMethodNotAllowed)),
	middleware.RequestLogger(logger),
	middleware.Recover(logger),           // before Shed: a panic frees the slot and is still logged
	middleware.Shed(256),                 // 503 + Retry-After once 256 requests are in flight
	middleware.Timeout(10*time.Second),   // 504 problem+json when the handler returns on deadline
	middleware.CORS(corsOpts),
	middleware.CSRFProtect(middleware.WithTrustedOrigin("https://app.example.com")),
	middleware.Authorize(enforcer, extractRoles),
)
```

- `Shed` sizing: CPU-bound services use `GOMAXPROCS × target latency / CPU time per request`; I/O-bound services use the pool size of the narrowest dependency (for example the database's `MaxOpenConns`).
- `Timeout` injects a context deadline and answers 504 only if the handler returns on that deadline without having written anything; it does not interrupt handlers that ignore `ctx`. Keep it below `server.New`'s `WriteTimeout` (60s by default).

## Shutdown on Kubernetes

`server.Run` flips readiness through `OnShutdown`, keeps serving for `WithDrainDelay` (recommended 3–5s on Kubernetes so Endpoints propagate; 0 on ECS behind an ALB, which deregisters before SIGTERM), then calls `Shutdown` and drains in-flight requests for `WithShutdownTimeout`. Keep `drainDelay + shutdownTimeout` below `terminationGracePeriodSeconds`.

Services that run the HTTP server next to other components (consumers, schedulers) use `server.NewComponent(server.New(h), logger)` instead of `Run`: `Start(ctx, onFatal)` binds synchronously (a busy port fails startup) and serves in the background, a later serve failure goes to `onFatal` (or is logged when `onFatal` is nil), and `Stop(ctx)` drains in-flight requests. `Stop` runs neither `OnShutdown` hooks nor the drain delay; the orchestrator owns that order. `Addr()` returns the bound address with `:0` resolved.

## Packages

| Package | What it does |
|---|---|
| `middleware` | `Chain` (alice-style), `TraceContext`, `RequestLogger`, `Recover`, `CORS`, `CSRFProtect`, `Authorize`, `InterceptStatus`, `Timeout`, `Shed` |
| `presenter` | `JSON`, `PlainText`, `Error` (RFC 9457), `Bind` (body capped at 1 MiB via `MaxBytesReader`, typed `*BindError` with 413/400) |
| `problem` | `Detail` (RFC 9457) + builder options + `Respond` |
| `health` | `Probe` (liveness/readiness with `Checker` interface, `WithCheckTimeout`) + `Status` (build/uptime) |
| `authz` | `Enforcer` interface + `Input` + `PayloadExtractor` type |
| `authz/opa` | OPA SDK adapter (`Client`, `IsAllowed`, `Query`, `Permissions`, `AllowedResources`) |
| `server` | Hardened `http.Server` defaults (anti-Slowloris timeouts, bounded headers, HTTP/2 limits) + signal-driven graceful shutdown with drain delay (`New`, `Run`, `OnShutdown`, `WithDrainDelay`) and a Start/Stop `Component` for orchestrated lifecycles; `net/http` internal errors routed to the structured logger |
| `logging` | Platform-standard logger: OTel/ECS field names, ISO 8601 UTC timestamps, automatic trace context injection, `Err` (error as a structured object) |
| `trace` | W3C Trace Context: `Parse`, generation, context propagation and a `Transport` for outbound calls |

## Reloadable OPA data

`authz/opa` can take its data document (`data.*`) from memory and replace it at runtime, e.g. from a config store, without restarting:

```go
c, err := opa.New(ctx, opa.Config{Query: "data.authz.allow", PoliciesPath: "policies/authz"}, opa.WithData(doc))
// later, on a config refresh:
if err := c.Reload(ctx, newDoc); err != nil {
	log.Error("reload failed", logging.Err(err)) // previous document keeps serving
}
```

In this mode only `.rego` files under `PoliciesPath` are loaded (parsed once at `New`, `_test.rego` included, as in file mode); `DataFiles` and `WithData` are mutually exclusive, and a nil document is an error at `New` — it never falls back to data files on disk.

## Cross-origin security

`middleware.CORS` and `middleware.CSRFProtect` solve different problems
and are designed to be stacked together:

```go
chain := middleware.New(
	middleware.CORS(corsOpts),                                // who can READ responses
	middleware.CSRFProtect(                                   // who can WRITE
		middleware.WithTrustedOrigin("https://app.example.com"),
	),
	middleware.Authorize(enforcer, extractRoles),
)
```

- `CORS` authorizes browsers to read cross-origin responses.
- `CSRFProtect` (Go 1.25's `net/http.CrossOriginProtection`) blocks
  cross-origin state-changing requests (POST/PUT/DELETE/PATCH).
- Together they let SPAs read data while preventing malicious sites from
  acting on the user's session.

## Binding request bodies

`presenter.Bind` reads at most `presenter.DefaultMaxBodyBytes` (1 MiB; override per call with `presenter.WithMaxBodyBytes`) and returns a `*presenter.BindError` whose `Status` is `413` when the limit is hit and `400` for malformed JSON or a failing `Bind` method:

```go
var in createUser
if err := presenter.Bind(w, r, &in); err != nil {
	var be *presenter.BindError
	status := http.StatusBadRequest
	if errors.As(err, &be) {
		status = be.Status
	}
	presenter.Error(w, r, err, presenter.WithStatus(status))
	return
}
```

## Roadmap / TODO

Planned work, grouped by theme and roughly ordered by priority. Items tagged
**(breaking)** change a public signature or default and should land before the
`v1.0.0` tag.

### Security & correctness
- [ ] **Secure headers middleware** — `SecureHeaders(opts...)`: `nosniff`,
  `X-Frame-Options: DENY`, a restrictive default CSP, opt-in HSTS. Satisfies
  DAST/OWASP baseline scans.
- [x] **CORS safe defaults** — `AllowAll` no longer enables credentials and
  `CORS` panics on the `*` + credentials combo (v1.4.0).

### Observability (LGTM stack)
- [ ] **OpenTelemetry tracing** — optional `otelmw` module: `otelhttp` wrapping,
  explicit W3C propagator, low-cardinality route tags. Kept out of the core to
  avoid pulling `go.opentelemetry.io/*` into every consumer's `go.sum`.
- [ ] **Trace-aware logging** — `RequestLogger` emitting `trace_id`/`span_id` via
  `InfoContext`, plus `ReplaceAttr`-based redaction of sensitive fields
  (`authorization`, `token`, `password`).
- [ ] **RED metrics + ops endpoints** — Prometheus/OTLP `/metrics` and a separate
  admin mux for `pprof`/`expvar` (never on the public mux).

### Ergonomics
- [ ] **Error-returning handler adapter** — `HandlerFunc` returning `error` with
  an `errors.As`-based domain-error → status mapping, keeping `net/http` out of
  the domain layer.

### Testing & structure
- [ ] **Security-path test coverage** — table-driven tests for `CORS` (~300 LOC,
  currently untested), `Authorize`, `TraceContext`, `Recover`, `RequestLogger`.
- [ ] **Modularize `authz/opa`** — split into its own Go module so the OPA
  dependency tree (logrus, jwx, gqlparser…) stays out of the core's `go.sum`.

## Versioning

`v1.1.0` is the current stable release. See [`CHANGELOG.md`](CHANGELOG.md)
for the full migration guide from pre-v1 import paths and identifiers.

## License

See `LICENSE`.
