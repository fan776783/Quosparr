//go:build windows

package droid

import (
	"context"

	"github.com/danieljoos/wincred"
)

var readCredential = func(target string) (string, error) {
	cred, err := wincred.GetGenericCredential(target)
	if err != nil || cred == nil {
		return "", err
	}
	return string(cred.CredentialBlob), nil
}

// SystemKey reads the keyring item droid stores in the Windows Credential
// Manager.
func SystemKey(ctx context.Context, account string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if account != KeychainAccountKeyring {
		return "", ErrKeyUnavailable
	}
	// Factory CLI writes keytar-style targets ("service/account"), while
	// go-keyring's Windows backend uses "service:account".
	targets := []string{
		KeychainService + "/" + account,
		KeychainService + ":" + account,
	}
	for _, target := range targets {
		v, err := readCredential(target)
		if err == nil && v != "" {
			return v, nil
		}
	}
	return "", ErrKeyUnavailable
}
