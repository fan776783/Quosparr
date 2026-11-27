//go:build windows

package droid

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func stubReadCredential(t *testing.T, fn func(target string) (string, error)) *[]string {
	t.Helper()
	orig := readCredential
	var calls []string
	readCredential = func(target string) (string, error) {
		calls = append(calls, target)
		return fn(target)
	}
	t.Cleanup(func() { readCredential = orig })
	return &calls
}

func TestSystemKeyWindows(t *testing.T) {
	keytarTarget := KeychainService + "/" + KeychainAccountKeyring
	keyringTarget := KeychainService + ":" + KeychainAccountKeyring

	t.Run("keytar target succeeds", func(t *testing.T) {
		calls := stubReadCredential(t, func(target string) (string, error) {
			if target == keytarTarget {
				return "synthetic-keytar-secret", nil
			}
			return "", errors.New("unexpected lookup")
		})
		got, err := SystemKey(context.Background(), KeychainAccountKeyring)
		if err != nil {
			t.Fatalf("SystemKey() error = %v, want nil", err)
		}
		if got != "synthetic-keytar-secret" {
			t.Fatalf("SystemKey() = %q, want %q", got, "synthetic-keytar-secret")
		}
		if want := []string{keytarTarget}; !slices.Equal(*calls, want) {
			t.Fatalf("calls = %v, want %v", *calls, want)
		}
	})

	t.Run("keytar error falls back to go-keyring target", func(t *testing.T) {
		calls := stubReadCredential(t, func(target string) (string, error) {
			if target == keytarTarget {
				return "", errors.New("element not found")
			}
			if target == keyringTarget {
				return "synthetic-keyring-secret", nil
			}
			return "", errors.New("unexpected lookup")
		})
		got, err := SystemKey(context.Background(), KeychainAccountKeyring)
		if err != nil {
			t.Fatalf("SystemKey() error = %v, want nil", err)
		}
		if got != "synthetic-keyring-secret" {
			t.Fatalf("SystemKey() = %q, want %q", got, "synthetic-keyring-secret")
		}
		if want := []string{keytarTarget, keyringTarget}; !slices.Equal(*calls, want) {
			t.Fatalf("calls = %v, want %v", *calls, want)
		}
	})

	t.Run("keytar empty string falls back to go-keyring target", func(t *testing.T) {
		calls := stubReadCredential(t, func(target string) (string, error) {
			if target == keytarTarget {
				return "", nil
			}
			if target == keyringTarget {
				return "synthetic-keyring-secret", nil
			}
			return "", errors.New("unexpected lookup")
		})
		got, err := SystemKey(context.Background(), KeychainAccountKeyring)
		if err != nil {
			t.Fatalf("SystemKey() error = %v, want nil", err)
		}
		if got != "synthetic-keyring-secret" {
			t.Fatalf("SystemKey() = %q, want %q", got, "synthetic-keyring-secret")
		}
		if want := []string{keytarTarget, keyringTarget}; !slices.Equal(*calls, want) {
			t.Fatalf("calls = %v, want %v", *calls, want)
		}
	})

	t.Run("both fail returns ErrKeyUnavailable", func(t *testing.T) {
		calls := stubReadCredential(t, func(target string) (string, error) {
			return "", errors.New("boom")
		})
		_, err := SystemKey(context.Background(), KeychainAccountKeyring)
		if !errors.Is(err, ErrKeyUnavailable) {
			t.Fatalf("SystemKey() error = %v, want ErrKeyUnavailable", err)
		}
		if want := []string{keytarTarget, keyringTarget}; !slices.Equal(*calls, want) {
			t.Fatalf("calls = %v, want %v", *calls, want)
		}
	})

	t.Run("security-cli account returns ErrKeyUnavailable without reading", func(t *testing.T) {
		calls := stubReadCredential(t, func(target string) (string, error) {
			return "should-not-read", nil
		})
		_, err := SystemKey(context.Background(), KeychainAccountSecurityCLI)
		if !errors.Is(err, ErrKeyUnavailable) {
			t.Fatalf("SystemKey() error = %v, want ErrKeyUnavailable", err)
		}
		if len(*calls) != 0 {
			t.Fatalf("calls = %v, want none", *calls)
		}
	})

	t.Run("canceled context returns context.Canceled without reading", func(t *testing.T) {
		calls := stubReadCredential(t, func(target string) (string, error) {
			return "should-not-read", nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := SystemKey(ctx, KeychainAccountKeyring)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SystemKey() error = %v, want context.Canceled", err)
		}
		if len(*calls) != 0 {
			t.Fatalf("calls = %v, want none", *calls)
		}
	})
}
