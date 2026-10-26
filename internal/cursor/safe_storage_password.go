package cursor

import (
	"bytes"
	"context"
	"errors"
	"io"
)

// No command output or OS error text escapes the credential boundary.
func readSafeStoragePassword(ctx context.Context, run func(io.Writer) error) ([]byte, error) {
	out := &passwordOutput{}
	defer clear(out.data)
	err := run(out)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if out.overflow {
		return nil, ErrKeychainUnavailable
	}
	if err != nil {
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && (exit.ExitCode() == 51 || exit.ExitCode() == 128) {
			return nil, ErrAuthorizationDenied
		}
		return nil, ErrKeychainUnavailable
	}
	p := bytes.TrimRight(out.data, "\r\n")
	if len(p) == 0 || len(p) > 4095 {
		return nil, ErrKeychainUnavailable
	}
	return append([]byte(nil), p...), nil
}

type passwordOutput struct {
	data     []byte
	overflow bool
}

func (w *passwordOutput) Write(p []byte) (int, error) {
	if w.overflow || len(w.data)+len(p) > 4097 {
		w.overflow = true
		return 0, ErrKeychainUnavailable
	}
	w.data = append(w.data, p...)
	return len(p), nil
}
