# httpgate

[![Go Reference](https://pkg.go.dev/badge/github.com/norlis/httpgate.svg)](https://pkg.go.dev/github.com/norlis/httpgate)

A small, idiomatic Go library of `net/http` building blocks for services that expose HTTP APIs: middlewares (TraceContext for W3C Trace Context, logger, recover, CORS, CSRF protection, OPA-based authorization, status interceptor, timeout, load shedding), JSON / problem+json response helpers, RFC 9457 problem details, liveness/readiness probes, a hardened `http.Server` with graceful shutdown, and the platform-standard structured logger.

Built on the standard library plus `log/slog`; the only external dependency is OPA, in `authz/opa`.

> **Status:** stable (v1). Incompatible changes are still released in minor versions, each with its migration in the [CHANGELOG](CHANGELOG.md): pin a minor version and read the CHANGELOG on every upgrade.

## Install

```bash
go get github.com/norlis/httpgate@v1.5.0
```

Requires Go 1.27+.

## Quick start

A JSON endpoint behind trace context, access logging and panic recovery. This is `Example` in [`middleware/example_test.go`](middleware/example_test.go), compiled and checked by `go test`; a service then serves the handler with `server.Run`.

```go
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

// Hardened timeouts and header limits, graceful shutdown on SIGINT/SIGTERM.
if err := server.Run(ctx, server.New(chain.Then(mux), server.WithAddr(":8080"))); err != nil {
	logger.Error("server stopped", logging.Err(err))
}
```

See [`examples/basic/`](examples/basic/) for a full setup with OPA-based authorization, CSRF protection, graceful shutdown, and a working roles extractor.

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

## Documentation

- [Guide](docs/guide.md): recommended middleware order, shutdown on Kubernetes, reloadable OPA data, cross-origin security, binding request bodies.
- [Logging catalog](docs/logging.md): fields and messages of the platform logging standard.
- [API reference](https://pkg.go.dev/github.com/norlis/httpgate) on pkg.go.dev.
- [CHANGELOG](CHANGELOG.md): changes and migrations per version, including the move from pre-v1 import paths.
- [Roadmap](ROADMAP.md): planned work.

## Contributing

Setup, checks and the release process are in [CONTRIBUTING.md](CONTRIBUTING.md).

## License

See `LICENSE`.
