package logging

import (
	"context"
	"log/slog"

	"github.com/norlis/httpgate/trace"
)

// handler decorates a slog.Handler so every record logged through a *Context
// method gains trace_id and span_id when the context carries them — business
// code never passes them manually (§2.3).
type handler struct {
	inner slog.Handler
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if tc, ok := trace.FromContext(ctx); ok {
		r = r.Clone()
		r.AddAttrs(slog.String(KeyTraceID, tc.TraceID), slog.String(KeySpanID, tc.SpanID))
	}
	return h.inner.Handle(ctx, r) //nolint:wrapcheck // pass-through of the wrapped slog.Handler's error; wrapping would break the interface contract
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup opens a group on the inner handler. Trace fields injected after
// an open group land inside it — avoid WithGroup on a service's root logger.
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name)}
}
