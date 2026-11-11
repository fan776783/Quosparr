//go:build darwin

package copilot

import (
	"context"
	"os/exec"
	"time"
)

func readPassword(ctx context.Context, service, account string) ([]byte, error) {
	args := []string{"find-generic-password", "-s", service, "-w"}
	if account != "" {
		args = append(args, "-a", account)
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	cmd.WaitDelay = time.Second
	data, err := cmd.Output()
	if err != nil || len(data) > 16384 {
		clear(data)
		return nil, ErrAuthorize
	}
	for len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r') {
		data = data[:len(data)-1]
	}
	return data, nil
}
