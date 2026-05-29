# httpgate

A small, idiomatic Go library of `net/http` building blocks: middlewares (trace, logger, recover, CORS, CSRF protection, OPA-based authorization, status interceptor), JSON / problem+json response helpers, RFC 7807 problem details, and liveness/readiness probes.

Built on the standard library plus `log/slog` for logging.

## Install

```bash
go get github.com/norlis/httpgate@v1.0.0
```

Requires Go 1.26+.

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
| `presenter` | `JSON`, `PlainText`, `Error` (RFC 7807), `Bind`, `Render` |
| `problem` | `Detail` (RFC 7807) + builder options + `Respond` |
| `health` | `Probe` (liveness/readiness with `Checker` interface) + `Status` (build/uptime) |
| `authz` | `Enforcer` interface + `Input` + `PayloadExtractor` type |
| `authz/opa` | OPA SDK adapter (`Client`, `IsAllowed`, `Data` introspection) |

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

## Versioning

`v1.0.0` is the first stable release. See [`CHANGELOG.md`](CHANGELOG.md)
for the full migration guide from pre-v1 import paths and identifiers.

## License

See `LICENSE`.
