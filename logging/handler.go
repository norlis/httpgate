package logging

import (
	"context"
	"log/slog"
	"time"

	"github.com/norlis/httpgate/trace"
)

// handler decorates a slog.Handler so every record logged through a *Context
// method gains trace_id and span_id when the context carries them — business
// code never passes them manually (§2.3). It also mirrors a top-level
// event.duration attr into event.duration_human for human reading; a duration
// bound with WithAttrs, written inside a group, or carried by an unresolved
// LogValuer is not mirrored.
type handler struct {
	inner slog.Handler
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	tc, hasTrace := trace.FromContext(ctx)
	d, hasDuration := eventDuration(r)

	if hasTrace || hasDuration {
		r = r.Clone()
		if hasTrace {
			r.AddAttrs(slog.String(KeyTraceID, tc.TraceID), slog.String(KeySpanID, tc.SpanID))
		}
		if hasDuration {
			r.AddAttrs(slog.String(KeyEventDurationHuman, d.String()))
		}
	}
	return h.inner.Handle(ctx, r) //nolint:wrapcheck // pass-through of the wrapped slog.Handler's error; wrapping would break the interface contract
}

// eventDuration finds the top-level KeyEventDuration attr. Int64 carries
// nanoseconds per the standard; KindDuration is accepted for robustness. Any
// other kind is left alone: a mistyped attr must never break logging.
func eventDuration(r slog.Record) (time.Duration, bool) {
	var d time.Duration
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != KeyEventDuration {
			return true
		}
		switch a.Value.Kind() {
		case slog.KindInt64:
			d, found = time.Duration(a.Value.Int64()), true
		case slog.KindDuration:
			d, found = a.Value.Duration(), true
		}
		return false
	})
	return d, found
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup opens a group on the inner handler. Trace fields injected after
// an open group land inside it — avoid WithGroup on a service's root logger.
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name)}
}
