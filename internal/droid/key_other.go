//go:build !darwin

package droid

import (
	"context"

	"github.com/zalando/go-keyring"
)

// SystemKey reads the keyring item droid stores through the Windows
// Credential Manager or the Linux Secret Service.
func SystemKey(ctx context.Context, account string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if account != KeychainAccountKeyring {
		return "", ErrKeyUnavailable
	}
	v, err := keyring.Get(KeychainService, account)
	if err != nil {
		return "", ErrKeyUnavailable
	}
	return v, nil
}
