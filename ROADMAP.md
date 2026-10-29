# Roadmap

Planned work, grouped by theme and roughly ordered by priority. Items tagged **(breaking)** change a public signature or default and should land before the `v1.0.0` tag.

## Security & correctness
- [ ] **Secure headers middleware** — `SecureHeaders(opts...)`: `nosniff`, `X-Frame-Options: DENY`, a restrictive default CSP, opt-in HSTS. Satisfies DAST/OWASP baseline scans.
- [x] **CORS safe defaults** — `AllowAll` no longer enables credentials and `CORS` panics on the `*` + credentials combo (v1.4.0).

## Observability (LGTM stack)
- [ ] **OpenTelemetry tracing** — optional `otelmw` module: `otelhttp` wrapping, explicit W3C propagator, low-cardinality route tags. Kept out of the core to avoid pulling `go.opentelemetry.io/*` into every consumer's `go.sum`.
- [ ] **Trace-aware logging** — `RequestLogger` emitting `trace_id`/`span_id` via `InfoContext`, plus `ReplaceAttr`-based redaction of sensitive fields (`authorization`, `token`, `password`).
- [ ] **RED metrics + ops endpoints** — Prometheus/OTLP `/metrics` and a separate admin mux for `pprof`/`expvar` (never on the public mux).

## Ergonomics
- [ ] **Error-returning handler adapter** — `HandlerFunc` returning `error` with an `errors.As`-based domain-error → status mapping, keeping `net/http` out of the domain layer.

## Testing & structure
- [ ] **Security-path test coverage** — table-driven tests for `CORS` (~300 LOC, currently untested), `Authorize`, `TraceContext`, `Recover`, `RequestLogger`.
- [ ] **Modularize `authz/opa`** — split into its own Go module so the OPA dependency tree (logrus, jwx, gqlparser…) stays out of the core's `go.sum`.
