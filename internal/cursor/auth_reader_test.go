package cursor

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/sqliteruntime"
)

const knownSealed = "cursor-safe-storage:v1:djEwj7fYBv6TrqFFx8+pf/dMHn3hvAK+DcFE63rHML7MmGGJ/wRYj7wtnuvLZij9WjbrgGkyPd8sDIWG8MdFwQt89tw4AzFtPTueIp+5uUFhT4Hgt0HLKhykt38p7zRSCG6f"
const knownToken = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhdXRoMHxzeW50aGV0aWMiLCJleHAiOjE4OTM0NTYwMDB9.c3ludGhldGlj"

var authTestNow = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

func sealedFixture(password, subject string, expires int64) string {
	body, _ := json.Marshal(map[string]any{"sub": subject, "exp": expires})
	token := "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(body) + ".c3ludGhldGlj"
	p := []byte(token)
	pad := aes.BlockSize - len(p)%aes.BlockSize
	p = append(p, bytes.Repeat([]byte{byte(pad)}, pad)...)
	k := deriveSafeStorageKey([]byte(password))
	defer clear(k)
	b, _ := aes.NewCipher(k)
	out := make([]byte, len(p))
	cipher.NewCBCEncrypter(b, []byte("                ")).CryptBlocks(out, p)
	return safeStoragePrefix + base64.StdEncoding.EncodeToString(append([]byte("v10"), out...))
}

func testAuthReader(t *testing.T) (*AuthReader, func(string, string), *atomic.Int32) {
	t.Helper()
	if err := sqliteruntime.Load(); err != nil {
		if os.Getenv("USAGE_REQUIRE_SQLITE") == "1" {
			t.Fatal(err)
		}
		t.Skip("SQLite unavailable")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "User", "globalStorage", "state.vscdb")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), path, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Execute(context.Background(), "CREATE TABLE ItemTable (key TEXT PRIMARY KEY,value BLOB)"); err != nil {
		t.Fatal(err)
	}
	put := func(k, v string) {
		t.Helper()
		if _, err := db.Execute(context.Background(), "INSERT OR REPLACE INTO ItemTable VALUES (?, ?)", k, v); err != nil {
			t.Fatal(err)
		}
	}
	put("cursorAuth/accessToken", knownSealed)
	put("cursorAuth/cachedEmail", "synthetic@example.invalid")
	r := NewAuthReader(dir)
	r.supported = true
	r.now = func() time.Time { return authTestNow }
	calls := &atomic.Int32{}
	r.password = func(context.Context) ([]byte, error) { calls.Add(1); return []byte("synthetic-password"), nil }
	t.Cleanup(r.Clear)
	return r, put, calls
}

func TestSafeStorageIndependentKnownVectorAndFormatBoundaries(t *testing.T) {
	key := deriveSafeStorageKey([]byte("synthetic-password"))
	defer clear(key)
	if hex.EncodeToString(key) != "ad47d040c184e403f3afc7b04054ec52" {
		t.Fatal("PBKDF2 differs from independent vector")
	}
	b, err := safeStorageCiphertext(knownSealed)
	if err != nil {
		t.Fatal(err)
	}
	token, err := decryptSafeStorage(b, key, authTestNow)
	if err != nil || token != knownToken {
		t.Fatal("independent cipher fixture failed")
	}
	for _, value := range []string{"cursor-safe-storage:v2:AAAA", safeStoragePrefix + "not base64", safeStoragePrefix + base64.StdEncoding.EncodeToString([]byte("v11")), safeStoragePrefix + strings.Repeat("A", 16384)} {
		if _, err := safeStorageCiphertext(value); err == nil {
			t.Fatal("invalid encrypted format accepted")
		}
	}
	b[len(b)-1] ^= 1
	if _, err := decryptSafeStorage(b, key, authTestNow); err == nil {
		t.Fatal("damaged ciphertext accepted")
	}
	if _, err := decryptSafeStorage([]byte{1}, key, authTestNow); err == nil {
		t.Fatal("partial block accepted")
	}
}

func TestAuthReaderNeedsExplicitAuthorizationAndFollowsRotation(t *testing.T) {
	r, put, calls := testAuthReader(t)
	path := filepath.Join(r.dir, "User", "globalStorage", "state.vscdb")
	before, _ := os.ReadFile(path)
	for i := 0; i < 3; i++ {
		if _, err := r.Read(context.Background()); !errors.Is(err, ErrAuthorizationRequired) {
			t.Fatal("background read did not request explicit consent")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("background touched Keychain")
	}
	if err := r.Authorize(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, err := r.Read(context.Background())
	if err != nil || a.Token != knownToken || calls.Load() != 1 {
		t.Fatal("authorized read failed")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("login database changed")
	}
	put("cursorAuth/accessToken", sealedFixture("synthetic-password", "auth0|second-account", authTestNow.Add(time.Hour).Unix()))
	put("cursorAuth/cachedEmail", "second@example.invalid")
	next, err := r.Read(context.Background())
	if err != nil || next.Token == a.Token || next.ID() == a.ID() || calls.Load() != 1 {
		t.Fatal("rotation/account change did not use current desktop data")
	}
	if err := r.Authorize(context.Background()); err != nil || calls.Load() != 1 {
		t.Fatal("cached authorization read Keychain again")
	}
	key := r.key
	r.Clear()
	if !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatal("derived key was not cleared")
	}
	if _, err := r.Read(context.Background()); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatal("cleared session remained authorized")
	}
}

func TestAuthReaderDenialDoesNotRetryUntilUserAuthorizes(t *testing.T) {
	r, _, calls := testAuthReader(t)
	r.password = func(context.Context) ([]byte, error) { calls.Add(1); return nil, ErrAuthorizationDenied }
	if err := r.Authorize(context.Background()); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := r.Read(context.Background()); !errors.Is(err, ErrAuthorizationDenied) {
			t.Fatal("denial missing")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("denied background read retried")
	}
	r.password = func(context.Context) ([]byte, error) { calls.Add(1); return []byte("synthetic-password"), nil }
	if err := r.Authorize(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatal("explicit retry failed")
	}
}

func TestAuthReaderKeyRotationRequiresExplicitRetry(t *testing.T) {
	r, put, calls := testAuthReader(t)
	if err := r.Authorize(context.Background()); err != nil {
		t.Fatal(err)
	}
	put("cursorAuth/accessToken", sealedFixture("rotated-key", "auth0|synthetic", authTestNow.Add(time.Hour).Unix()))
	if _, err := r.Read(context.Background()); !errors.Is(err, ErrAuthorizationRequired) || calls.Load() != 1 {
		t.Fatal("key rotation retried silently or remained readable")
	}
	r.password = func(context.Context) ([]byte, error) { calls.Add(1); return []byte("rotated-key"), nil }
	if err := r.Authorize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatal("rotated key unavailable")
	}
}

func TestAuthReaderInvalidModesNeverAccessKeychain(t *testing.T) {
	for _, mode := range []string{"isolated", "unknown-version", "other-platform", "missing-token", "expired", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			r, put, calls := testAuthReader(t)
			switch mode {
			case "isolated":
				put("cursorAuth/activeDeploymentId", "isolated")
			case "unknown-version":
				put("cursorAuth/accessToken", "cursor-safe-storage:v2:abc")
			case "other-platform":
				r.supported = false
			case "missing-token":
				put("cursorAuth/accessToken", "")
			case "malformed":
				put("cursorAuth/accessToken", safeStoragePrefix+"!!")
			case "expired":
				put("cursorAuth/accessToken", sealedFixture("synthetic-password", "a", authTestNow.Add(-time.Hour).Unix()))
			}
			err := r.Authorize(context.Background())
			if err == nil {
				t.Fatal("invalid mode accepted")
			}
			if mode == "expired" {
				if !errors.Is(err, ErrUnauthorized) || len(r.key) != 0 {
					t.Fatal("expired token accepted/cached")
				}
			} else if calls.Load() != 0 {
				t.Fatal("invalid source reached Keychain")
			}
		})
	}
}

func TestAuthReaderClearLogoutAndCancellationDuringAuthorization(t *testing.T) {
	for _, change := range []string{"clear", "logout", "isolated", "cancel"} {
		t.Run(change, func(t *testing.T) {
			r, put, _ := testAuthReader(t)
			started, release := make(chan struct{}), make(chan struct{})
			r.password = func(context.Context) ([]byte, error) {
				close(started)
				<-release
				return []byte("synthetic-password"), nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.Authorize(ctx) }()
			<-started
			if err := r.Authorize(context.Background()); !errors.Is(err, ErrAuthorizationRequired) {
				t.Fatal("concurrent prompt was not suppressed")
			}
			switch change {
			case "clear":
				r.Clear()
			case "logout":
				put("cursorAuth/accessToken", "")
			case "isolated":
				put("cursorAuth/activeDeploymentId", "isolated")
			case "cancel":
				cancel()
			}
			close(release)
			if err := <-done; err == nil {
				t.Fatal("late authorization succeeded")
			}
			if len(r.key) != 0 {
				t.Fatal("late key retained")
			}
		})
	}
}

type secretExitError int

func (e secretExitError) Error() string { return "synthetic-secret-output-must-not-escape" }
func (e secretExitError) ExitCode() int { return int(e) }

func TestSafeStorageCommandOutputIsBoundedAndErrorsAreSanitized(t *testing.T) {
	for _, code := range []int{0, 44, 51, 128, 1} {
		p, err := readSafeStoragePassword(context.Background(), func(w io.Writer) error {
			io.WriteString(w, "synthetic-password\n")
			if code != 0 {
				return secretExitError(code)
			}
			return nil
		})
		if code == 0 {
			if string(p) != "synthetic-password" || err != nil {
				t.Fatal("password normalization failed")
			}
			clear(p)
			continue
		}
		if len(p) != 0 || err == nil || strings.Contains(err.Error(), "synthetic") {
			t.Fatal("command secret leaked")
		}
		if (code == 51 || code == 128) != errors.Is(err, ErrAuthorizationDenied) {
			t.Fatal("denial classification incorrect")
		}
	}
	for _, n := range []int{0, 4096, 10000} {
		if _, err := readSafeStoragePassword(context.Background(), func(w io.Writer) error { _, e := w.Write(bytes.Repeat([]byte("x"), n)); return e }); !errors.Is(err, ErrKeychainUnavailable) {
			t.Fatal("unbounded password accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readSafeStoragePassword(ctx, func(io.Writer) error { return secretExitError(51) }); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation classified as denial")
	}
	r := NewAuthReader("")
	r.key = []byte("must-not-print")
	if strings.Contains(fmt.Sprintf("%#v", r), "must-not-print") {
		t.Fatal("reader disclosed key")
	}
}
