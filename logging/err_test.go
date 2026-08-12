package logging

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type timeoutError struct{}

func (timeoutError) Error() string { return "upstream timed out after 5s" }

func TestErr_StructuredObject(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Error("bank authorization call failed", Err(timeoutError{}))

	m := lastLine(t, buf)
	if m[KeyErrorType] != "logging.timeoutError" {
		t.Fatalf("error.type = %v", m[KeyErrorType])
	}
	if m[KeyErrorMessage] != "upstream timed out after 5s" {
		t.Fatalf("error.message = %v", m[KeyErrorMessage])
	}
	stack, _ := m[KeyErrorStackTrace].(string)
	if !strings.Contains(stack, "goroutine") || !strings.Contains(stack, "\n") {
		t.Fatalf("error.stack_trace must be a single string with embedded newlines, got %q", stack)
	}
}

func TestErr_UnwrapsToRootCause(t *testing.T) {
	t.Parallel()
	root := timeoutError{}
	wrapped := fmt.Errorf("payment authorization failed: %w", fmt.Errorf("bank call: %w", root))

	buf := &bytes.Buffer{}
	New(buf).Error("event", Err(wrapped))

	m := lastLine(t, buf)
	if m[KeyErrorType] != "logging.timeoutError" {
		t.Fatalf("error.type must be the root cause type, got %v", m[KeyErrorType])
	}
	msg, _ := m[KeyErrorMessage].(string)
	if !strings.Contains(msg, "payment authorization failed") || !strings.Contains(msg, "upstream timed out") {
		t.Fatalf("error.message must keep the full cause chain, got %q", msg)
	}
}

func TestErr_NilIsDropped(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Error("event", Err(nil))

	m := lastLine(t, buf)
	if _, ok := m[KeyErrorType]; ok {
		t.Fatalf("nil error must not emit error fields: %v", m)
	}
}

func TestErr_StdlibErrorType(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Error("event", Err(errors.New("boom")))

	m := lastLine(t, buf)
	if m[KeyErrorType] != "*errors.errorString" {
		t.Fatalf("error.type = %v", m[KeyErrorType])
	}
}
