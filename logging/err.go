package logging

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// Err renders err as the standard's structured error object (§2.7):
// error.type (the root cause's Go type), error.message (the full message,
// cause chain included) and error.stack_trace (captured at the call site as a
// single string). A nil err yields an empty attr that handlers drop.
//
// Caveat: error.stack_trace is captured here, at the Err() call site, not
// where the error originally arose — so it shows the logging call path, not
// the failure's origin. Wrap errors with context (%w) if you need the origin.
func Err(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	// The empty group key makes the JSON handler inline these at top level,
	// producing the standard's flat dotted keys.
	return slog.Attr{Key: "", Value: slog.GroupValue(
		slog.String(KeyErrorType, errorType(err)),
		slog.String(KeyErrorMessage, err.Error()),
		slog.String(KeyErrorStackTrace, string(debug.Stack())),
	)}
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
