# httpgate

A small, idiomatic Go library of `net/http` building blocks: middlewares (trace, logger, recover, CORS, CSRF protection, OPA-based authorization, status interceptor), JSON / problem+json response helpers, RFC 9457 problem details, and liveness/readiness probes.

Built on the standard library plus `log/slog` for logging.

## Install

```bash
go get github.com/norlis/httpgate@v1.0.0
```

Requires Go 1.25.1+.

## Quick start

```go
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/norlis/httpgate/middleware"
	"github.com/norlis/httpgate/presenter"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	chain := middleware.New(
		middleware.TraceID(),
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

## Packages

| Package | What it does |
|---|---|
| `middleware` | `Chain` (alice-style), `TraceID`, `RequestLogger`, `Recover`, `CORS`, `CSRFProtect`, `Authorize`, `InterceptStatus` |
| `presenter` | `JSON`, `PlainText`, `Error` (RFC 9457), `Bind`, `Render` |
| `problem` | `Detail` (RFC 9457) + builder options + `Respond` |
| `health` | `Probe` (liveness/readiness with `Checker` interface) + `Status` (build/uptime) |
| `authz` | `Enforcer` interface + `Input` + `PayloadExtractor` type |
| `authz/opa` | OPA SDK adapter (`Client`, `IsAllowed`, `Query`, `Permissions`, `AllowedResources`) |
| `server` | Hardened `http.Server` defaults (anti-Slowloris timeouts) + signal-driven graceful shutdown (`New`, `Run`, `OnShutdown`) |

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

## Roadmap / TODO

Planned work, grouped by theme and roughly ordered by priority. Items tagged
**(breaking)** change a public signature or default and should land before the
`v1.0.0` tag.

### Security & correctness
- [ ] **Harden JSON decoding** — `Bind` / `DecodeJSON[T]` with `MaxBytesReader`
  (body-size DoS guard), typed decode errors mapped to problem+json (400/413),
  and opt-in `DisallowUnknownFields`. **(breaking: `Bind` gains a `w` arg)**
- [ ] **Secure headers middleware** — `SecureHeaders(opts...)`: `nosniff`,
  `X-Frame-Options: DENY`, a restrictive default CSP, opt-in HSTS. Satisfies
  DAST/OWASP baseline scans.
- [ ] **CORS safe defaults** — `AllowAll` must not enable credentials with
  wildcard origins; refuse the `*` + credentials combo. **(breaking: default change)**

### Observability (LGTM stack)
- [ ] **OpenTelemetry tracing** — optional `otelmw` module: `otelhttp` wrapping,
  explicit W3C propagator, low-cardinality route tags. Kept out of the core to
  avoid pulling `go.opentelemetry.io/*` into every consumer's `go.sum`.
- [ ] **Trace-aware logging** — `RequestLogger` emitting `trace_id`/`span_id` via
  `InfoContext`, plus `ReplaceAttr`-based redaction of sensitive fields
  (`authorization`, `token`, `password`).
- [ ] **RED metrics + ops endpoints** — Prometheus/OTLP `/metrics` and a separate
  admin mux for `pprof`/`expvar` (never on the public mux).

### Resilience
- [ ] **Per-route timeout middleware** — graceful 504 problem+json instead of an
  abrupt TCP reset, by injecting a context deadline and responding only if the
  handler hasn't written yet — **no full-response buffering**, so streaming/SSE
  keep working (unlike a naive `httptest.Recorder` approach).
- [ ] **Load shedding (bulkhead)** — a concurrency-limiting semaphore
  (`chan struct{}`, stdlib) that rejects new requests with `503` + `Retry-After`
  (problem+json) once N are in-flight, protecting the container from OOM/CPU
  exhaustion under overload. Non-blocking shed by default; optional short queue
  to absorb bursts. Composes with the per-route timeout.

### Ergonomics
- [ ] **Error-returning handler adapter** — `HandlerFunc` returning `error` with
  an `errors.As`-based domain-error → status mapping, keeping `net/http` out of
  the domain layer.

### Testing & structure
- [ ] **Security-path test coverage** — table-driven tests for `CORS` (~300 LOC,
  currently untested), `Authorize`, `TraceID`, `Recover`, `RequestLogger`.
- [ ] **Modularize `authz/opa`** — split into its own Go module so the OPA
  dependency tree (logrus, jwx, gqlparser…) stays out of the core's `go.sum`.

## Versioning

`v1.0.0` is the first stable release. See [`CHANGELOG.md`](CHANGELOG.md)
for the full migration guide from pre-v1 import paths and identifiers.

## License

See `LICENSE`.
