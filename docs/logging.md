# Catálogo de logging

httpgate implementa el Estándar de Logging para Microservicios v1.0. Este es el catálogo de campos y mensajes que emite la librería.

## Campos

| Campo | Tipo | Origen | Notas |
|---|---|---|---|
| `timestamp` | string | handler `logging` | ISO 8601 UTC con milisegundos y sufijo `Z` |
| `log.level` | string | handler `logging` | `debug` \| `info` \| `warn` \| `error`, minúsculas |
| `message` | string | cada call-site | estático, en inglés, sin variables interpoladas |
| `service.name` | string | `logging.WithService` | presente si fue configurado |
| `service.version` | string | `logging.WithService` | presente si fue configurado |
| `deployment.environment.name` | string | `logging.WithEnvironment` | presente si fue configurado |
| `trace_id` | string | handler `logging` + `middleware.TraceContext` | 32 hex W3C; requiere usar los métodos `*Context` del logger |
| `span_id` | string | handler `logging` + `middleware.TraceContext` | 16 hex W3C |
| `http.request.method` | string | `middleware.RequestLogger` | |
| `url.path` | string | `middleware.RequestLogger` | sin query string |
| `http.response.status_code` | number | `RequestLogger`, `presenter.Error` | |
| `http.response.body.size` | number | `middleware.RequestLogger` | bytes escritos |
| `client.address` | string | `middleware.RequestLogger` | remote addr |
| `event.duration` | number | `middleware.RequestLogger` | **nanosegundos**, int64 |
| `error.type` | string | `logging.Err`, `Recover` | tipo Go de la causa raíz; `"panic"` en Recover |
| `error.message` | string | `logging.Err`, `Recover` | mensaje completo con cadena de causas |
| `error.stack_trace` | string | `logging.Err`, `Recover` | string único con `\n` embebidos |
| `server.address` | string | `server.Run` | |
| `shutdown.timeout` | number | `server.Run` | nanosegundos |
| `query` | string | `authz/opa` | query rego configurada |
| `cors.reason` | string | `middleware.CORS` (debug) | motivo de rechazo/skip |
| `cors.origin`, `cors.method`, `cors.headers` | string/array | `middleware.CORS` (debug) | |

## Mensajes

| Mensaje | Nivel | Emisor |
|---|---|---|
| `request completed` | info | `middleware.RequestLogger` (uno por petición) |
| `panic recovered` | error | `middleware.Recover` |
| `server error` | error | `presenter.Error` (5xx, una sola vez) |
| `json encoding failed` | error | `presenter.JSON` (via `slog.Default()`; requires `slog.SetDefault(logging.New(...))` to conform to the standard) |
| `problem encoding failed` | error | `problem.Respond` (via `slog.Default()`; requires `slog.SetDefault(logging.New(...))` to conform to the standard) |
| `server listening` | info | `server.Run` |
| `server draining` | info | `server.Run` |
| `preflight request handled` | debug | `middleware.CORS` |
| `preflight rejected` | debug | `middleware.CORS` |
| `cors headers skipped` | debug | `middleware.CORS` |

## Reglas para servicios consumidores

- Construir el logger con `logging.New` y loguear siempre con los métodos `*Context` (`InfoContext`, `ErrorContext`, ...) para heredar `trace_id`/`span_id`.
- Encadenar `middleware.TraceContext` primero, luego `middleware.RequestLogger`, y `middleware.Recover` DESPUÉS de `RequestLogger` (es decir, `RequestLogger` debe envolver a `Recover`) para que el 500 recuperado quede reflejado en `http.response.status_code` de la línea de acceso, en vez de quedar oculto por el status ya escrito hacia el `ResponseWriter` externo.
- Usar `logging.Err(err)` en todo log de nivel error; nunca `slog.Any("error", err)`.
- Propagar trazas salientes con `&http.Client{Transport: &trace.Transport{}}` y `http.NewRequestWithContext`.
- Campos de dominio propios: `snake_case` o notación de puntos, consistente entre servicios; documentarlos en el catálogo del equipo.
- `presenter.JSON` y `problem.Respond` loguean vía `slog.Default()`, no vía un logger inyectado explícitamente. Sus líneas solo cumplen el estándar si el servicio llama `slog.SetDefault(logging.New(...))` durante el arranque; de lo contrario emiten con el handler `slog` por defecto (texto, claves `time`/`level`/`msg`).
