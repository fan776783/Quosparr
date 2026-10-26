//go:build !darwin

package cursor

import "context"

const safeStorageSupported = false

func safeStoragePassword(context.Context) ([]byte, error) { return nil, ErrEncryptedStorage }
