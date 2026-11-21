//go:build darwin

package droid

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"time"
)

const (
	securityPath    = "/usr/bin/security"
	securityTimeout = 5 * time.Second
)

// SystemKey reads the key droid stores with /usr/bin/security. Only the
// security-cli item is read: the keyring item belongs to droid's process and
// reading it from here would prompt.
func SystemKey(ctx context.Context, account string) (string, error) {
	if account != KeychainAccountSecurityCLI {
		return "", ErrKeyUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, securityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityPath, "find-generic-password", "-s", KeychainService, "-a", account, "-w")
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		return "", ErrKeyUnavailable
	}
	return string(bytes.TrimSpace(out)), nil
}
