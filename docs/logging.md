# Logging catalog

httpgate implements the Microservices Logging Standard v1.0. This is the catalog of fields and messages the library emits.

## Fields

| Field | Type | Source | Notes |
|---|---|---|---|
| `timestamp` | string | `logging` handler | ISO 8601 UTC with milliseconds and a `Z` suffix |
| `log.level` | string | `logging` handler | `debug` \| `info` \| `warn` \| `error`, lowercase |
| `message` | string | each call-site | static, in English, no interpolated variables |
| `service.name` | string | `logging.WithService` | present if configured |
| `service.version` | string | `logging.WithService` | present if configured |
| `deployment.environment.name` | string | `logging.WithEnvironment` | present if configured |
| `trace_id` | string | `logging` handler + `middleware.TraceContext` | 32 hex W3C; requires the logger's `*Context` methods |
| `span_id` | string | `logging` handler + `middleware.TraceContext` | 16 hex W3C |
| `http.request.method` | string | `middleware.RequestLogger` | |
| `url.path` | string | `middleware.RequestLogger` | without query string |
| `http.response.status_code` | number | `RequestLogger`, `presenter.Error` | |
| `http.response.body.size` | number | `middleware.RequestLogger` | bytes written |
| `client.address` | string | `middleware.RequestLogger` | remote addr |
| `event.duration` | number | `middleware.RequestLogger` | **nanoseconds**, int64 |
| `event.duration_human` | string | `logging` handler | human-readable mirror of `event.duration` via `time.Duration.String()` (`"65.484041ms"`); **human reading only: never aggregate or alert on it**. Requires the duration to be a top-level record attr |
| `error.type` | string | `logging.Err`, `Recover` | Go type of the root cause; `"panic"` in Recover |
| `error.message` | string | `logging.Err`, `Recover` | full message with the chain of causes |
| `error.stack_trace` | string | `logging.Err`, `Recover` | single string with embedded `\n` |
| `server.address` | string | `server.Run` | |
| `shutdown.timeout` | number | `server.Run` | nanoseconds |
| `query` | string | `authz/opa` | configured rego query |
| `cors.reason` | string | `middleware.CORS` (debug) | reason for the rejection/skip |
| `cors.origin`, `cors.method`, `cors.headers` | string/array | `middleware.CORS` (debug) | |

## Messages

| Message | Level | Emitter |
|---|---|---|
| `request completed` | info | `middleware.RequestLogger` (one per request) |
| `panic recovered` | error | `middleware.Recover` |
| `server error` | error | `presenter.Error` (5xx, exactly once) |
| `json encoding failed` | error | `presenter.JSON` (via `slog.Default()`; requires `slog.SetDefault(logging.New(...))` to conform to the standard) |
| `problem encoding failed` | error | `problem.Respond` (via `slog.Default()`; requires `slog.SetDefault(logging.New(...))` to conform to the standard) |
| `server listening` | info | `server.Run` |
| `server draining` | info | `server.Run` |
| `preflight request handled` | debug | `middleware.CORS` |
| `preflight rejected` | debug | `middleware.CORS` |
| `cors headers skipped` | debug | `middleware.CORS` |

## Rules for consuming services

- Build the logger with `logging.New` and always log through the `*Context` methods (`InfoContext`, `ErrorContext`, ...) so `trace_id`/`span_id` are inherited.
- Chain `middleware.TraceContext` first, then `middleware.RequestLogger`, and `middleware.Recover` AFTER `RequestLogger` (that is, `RequestLogger` must wrap `Recover`) so the recovered 500 is reflected in the access line's `http.response.status_code` instead of being hidden by the status already written to the outer `ResponseWriter`.
- Use `logging.Err(err)` in every error-level log; never `slog.Any("error", err)`.
- Propagate outbound traces with `&http.Client{Transport: &trace.Transport{}}` and `http.NewRequestWithContext`.
- Your own domain fields: `snake_case` or dotted notation, consistent across services; document them in your team's catalog.
- `presenter.JSON` and `problem.Respond` log via `slog.Default()`, not via an explicitly injected logger. Their lines only conform to the standard if the service calls `slog.SetDefault(logging.New(...))` at startup; otherwise they emit through the default `slog` handler (text, keys `time`/`level`/`msg`).
