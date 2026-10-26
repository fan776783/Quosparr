package cursor

import (
	"context"
	"errors"
	"sync"
	"time"
)

// AuthReader owns one monitoring session's derived key. Read never requests
// Keychain access. Only an explicit Authorize call may display a system prompt.
type AuthReader struct {
	dir         string
	mu          sync.Mutex
	authorizing sync.Mutex
	key         []byte
	denied      bool
	generation  uint64
	supported   bool
	password    func(context.Context) ([]byte, error)
	now         func() time.Time
}

func NewAuthReader(dir string) *AuthReader {
	return &AuthReader{dir: dir, supported: safeStorageSupported, password: safeStoragePassword, now: time.Now}
}

func (*AuthReader) String() string   { return "cursor credential reader (redacted)" }
func (*AuthReader) GoString() string { return "cursor credential reader (redacted)" }

// Clear invalidates in-flight authorization and erases the cached key.
func (r *AuthReader) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generation++
	clear(r.key)
	r.key, r.denied = nil, false
}

func (r *AuthReader) Read(ctx context.Context) (Auth, error) {
	a, err := readAuth(ctx, r.dir, r.unseal)
	if errors.Is(err, ErrNotLoggedIn) || errors.Is(err, ErrUnsupportedLogin) {
		r.Clear()
	}
	return a, err
}

func (r *AuthReader) unseal(value string) (string, error) {
	if !r.supported {
		return "", ErrEncryptedStorage
	}
	sealed, err := safeStorageCiphertext(value)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.key) == 0 {
		if r.denied {
			return "", ErrAuthorizationDenied
		}
		return "", ErrAuthorizationRequired
	}
	token, err := decryptSafeStorage(sealed, r.key, r.now())
	if err != nil && !errors.Is(err, ErrUnauthorized) {
		clear(r.key)
		r.key = nil
		return "", ErrAuthorizationRequired
	}
	return token, err
}

// Authorize serializes requests, rechecks the current login before caching, and
// does not retain the Keychain password. A failed/cancelled attempt is never
// retried by Read, including after key rotation.
func (r *AuthReader) Authorize(ctx context.Context) error {
	if !r.authorizing.TryLock() {
		return ErrAuthorizationRequired
	}
	defer r.authorizing.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	gen := r.generation
	r.denied = false
	r.mu.Unlock()
	if _, err := r.Read(ctx); !errors.Is(err, ErrAuthorizationRequired) {
		return err
	}
	password, err := r.password(ctx)
	defer clear(password)
	if err != nil {
		r.mu.Lock()
		if gen == r.generation {
			r.denied = errors.Is(err, ErrAuthorizationDenied)
		}
		r.mu.Unlock()
		return err
	}
	if len(password) == 0 || len(password) > 4095 {
		return ErrKeychainUnavailable
	}
	key := deriveSafeStorageKey(password)
	defer clear(key)
	used := false
	_, err = readAuth(ctx, r.dir, func(value string) (string, error) {
		sealed, err := safeStorageCiphertext(value)
		if err != nil {
			return "", err
		}
		used = true
		return decryptSafeStorage(sealed, key, r.now())
	})
	if errors.Is(err, ErrCredentials) {
		return ErrKeychainUnavailable
	}
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if gen != r.generation {
		return context.Canceled
	}
	if used {
		clear(r.key)
		r.key = append([]byte(nil), key...)
	}
	return nil
}
