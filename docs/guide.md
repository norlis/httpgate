# Guide

How to compose httpgate's pieces in a service. The [README](../README.md) covers installation and a first endpoint; the API reference is on [pkg.go.dev](https://pkg.go.dev/github.com/norlis/httpgate) and the log field catalog in [`logging.md`](logging.md).

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

`middleware.CORS` and `middleware.CSRFProtect` solve different problems and are designed to be stacked together:

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
- `CSRFProtect` (Go 1.25's `net/http.CrossOriginProtection`) blocks cross-origin state-changing requests (POST/PUT/DELETE/PATCH).
- Together they let SPAs read data while preventing malicious sites from acting on the user's session.

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
