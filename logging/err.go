package logging

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
)

// maxStackFrames bounds the frames captured by Err. Deep stacks past this
// point are framework noise; capping keeps the per-error cost predictable.
const maxStackFrames = 32

// Err renders err as the standard's structured error object (§2.7):
// error.type (the root cause's Go type), error.message (the full message,
// cause chain included) and error.stack_trace (up to maxStackFrames frames of
// the calling goroutine, captured at the Err call site, as a single string).
// A nil err yields an empty attr that handlers drop.
//
// Caveat: the stack shows the logging call path, not where the error arose.
// Wrap errors with context (%w) if you need the origin.
func Err(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	// The empty group key makes the JSON handler inline these at top level,
	// producing the standard's flat dotted keys.
	return slog.Attr{Key: "", Value: slog.GroupValue(
		slog.String(KeyErrorType, errorType(err)),
		slog.String(KeyErrorMessage, err.Error()),
		slog.String(KeyErrorStackTrace, stack()),
	)}
}

// stack formats the caller's frames like debug.Stack ("func\n\tfile:line\n")
// without its goroutine header and without walking past maxStackFrames.
func stack() string {
	var pcs [maxStackFrames]uintptr
	n := runtime.Callers(3, pcs[:]) // skip runtime.Callers, stack, Err
	frames := runtime.CallersFrames(pcs[:n])
	var b strings.Builder
	b.Grow(n * 160) // typical frame: ~40 bytes of symbol + ~100 of path; one allocation
	for {
		f, more := frames.Next()
		b.WriteString(f.Function)
		b.WriteString("\n\t")
		b.WriteString(f.File)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(f.Line))
		b.WriteByte('\n')
		if !more {
			break
		}
	}
	return b.String()
}

// errorType walks the single-error Unwrap chain to the root cause. Joined
// errors (Unwrap() []error) report the type of the join itself.
func errorType(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return fmt.Sprintf("%T", err)
		}
		err = next
	}
}
