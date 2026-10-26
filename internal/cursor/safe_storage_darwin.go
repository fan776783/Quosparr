//go:build darwin

package cursor

import (
	"context"
	"io"
	"os/exec"
	"time"
)

const safeStorageSupported = true

func safeStoragePassword(ctx context.Context) ([]byte, error) {
	return readSafeStoragePassword(ctx, func(out io.Writer) error {
		cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", "Cursor Safe Storage", "-w")
		cmd.Stdout, cmd.Stderr = out, io.Discard
		cmd.WaitDelay = time.Second
		return cmd.Run()
	})
}
