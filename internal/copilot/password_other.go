//go:build !darwin

package copilot

import "context"

func readPassword(context.Context, string, string) ([]byte, error) { return nil, ErrUnsupported }
