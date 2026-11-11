package copilot

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/sqliteruntime"
)

func TestCLIExplicitAuthorizationAccountBindingAndRotation(t *testing.T) {
	r := NewReader(t.TempDir())
	r.CLIDir = t.TempDir()
	calls := 0
	r.password = func(ctx context.Context, service, account string) ([]byte, error) {
		calls++
		if service != "copilot-cli" || account != "https://github.com:synthetic:github" {
			t.Fatal("credential account binding lost")
		}
		return []byte("synthetic-access"), nil
	}
	path := filepath.Join(r.CLIDir, "config.json")
	content := []byte("// managed state\n" + `{"lastLoggedInUser":{"host":"https://github.com","login":"synthetic"},"loggedInUsers":[{"host":"https://github.com","login":"synthetic","kind":"githubDotCom"},],}`)
	if e := os.WriteFile(path, content, 0600); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e := r.Read(context.Background(), CLI); !errors.Is(e, ErrAuthorize) {
			t.Fatal("background read did not require authorization")
		}
	}
	if calls != 0 {
		t.Fatal("background invoked Keychain")
	}
	a, e := r.Authorize(context.Background(), CLI)
	if e != nil || len(a) != 1 || a[0].Token != "synthetic-access" {
		t.Fatal("CLI authorization failed")
	}
	if _, e = r.Read(context.Background(), CLI); e != nil || calls != 1 {
		t.Fatal("cached authorization failed")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(content, after) {
		t.Fatal("CLI config was modified")
	}
	r.Clear()
	if _, e = r.Read(context.Background(), CLI); !errors.Is(e, ErrAuthorize) {
		t.Fatal("revocation retained credentials")
	}
	changed := strings.ReplaceAll(string(content), "synthetic", "changed")
	os.WriteFile(path, []byte(changed), 0600)
	if _, e = r.Read(context.Background(), CLI); !errors.Is(e, ErrAuthorize) {
		t.Fatal("another login inherited credentials")
	}
	data, _ := json.Marshal(a[0])
	if strings.Contains(string(data), "synthetic-access") || strings.Contains(fmt.Sprintf("%+v %#v", a[0], a[0]), "synthetic-access") {
		t.Fatal("credential escaped redaction")
	}
}
func TestVSCodeEncryptedSessionRequiresAuthorizationAndIsReadOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS safeStorage format")
	}
	if sqliteruntime.Load() != nil {
		t.Fatal("SQLite runtime unavailable")
	}
	r := NewReader(t.TempDir())
	r.VSCodeDir = t.TempDir()
	path := filepath.Join(r.VSCodeDir, "User", "globalStorage", "state.vscdb")
	os.MkdirAll(filepath.Dir(path), 0700)
	db, e := sqlite.Open(context.Background(), path, sqlite.OpenOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.Execute(context.Background(), "CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value BLOB)")
	plain := []byte(`[{"id":"session","account":{"id":"7","label":"synthetic"},"accessToken":"gho_synthetic_not_a_JWT","scopes":["read:user"]},{"id":"other","account":{"id":"8","label":"second"},"accessToken":"gho_second"}]`)
	k, _ := pbkdf2.Key(sha1.New, "synthetic-password", []byte("saltysalt"), 1003, 16)
	padded := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(16 - len(plain)%16)}, 16-len(plain)%16)...)
	block, _ := aes.NewCipher(k)
	sealed := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte("                ")).CryptBlocks(sealed, padded)
	raw := append([]byte("v10"), sealed...)
	ints := make([]int, len(raw))
	for i, v := range raw {
		ints[i] = int(v)
	}
	envelope, _ := json.Marshal(struct {
		Type string `json:"type"`
		Data []int  `json:"data"`
	}{"Buffer", ints})
	if _, e := db.Execute(context.Background(), "INSERT INTO ItemTable VALUES (?,?)", secretKey, string(envelope)); e != nil {
		t.Fatal(e)
	}
	// Real editor databases commonly exceed 1 MiB; only the secret row is bounded.
	if _, e := db.Execute(context.Background(), "INSERT INTO ItemTable VALUES (?,?)", "unrelated", strings.Repeat("x", 2<<20)); e != nil {
		t.Fatal(e)
	}
	calls := 0
	r.password = func(_ context.Context, service, account string) ([]byte, error) {
		calls++
		if service != "Code Safe Storage" || account != "" {
			t.Fatal("VS Code key scope lost")
		}
		return []byte("synthetic-password"), nil
	}
	if _, e := r.Read(context.Background(), VSCode); !errors.Is(e, ErrAuthorize) || calls != 0 {
		t.Fatal("VS Code background prompted")
	}
	a, e := r.Authorize(context.Background(), VSCode)
	if e != nil || len(a) != 2 || a[0].Token != "gho_synthetic_not_a_JWT" {
		t.Fatal("non-JWT GitHub sessions failed")
	}
	if _, e := r.Read(context.Background(), VSCode); e != nil || calls != 1 {
		t.Fatal("cached key failed")
	}
	rows, e := db.Query(context.Background(), "SELECT value FROM ItemTable WHERE key=?", secretKey)
	if e != nil || rows.Values[0][0] != string(envelope) {
		t.Fatal("VS Code database was modified")
	}
	cached := r.keys[VSCode]
	r.Clear()
	if !bytes.Equal(cached, make([]byte, len(cached))) {
		t.Fatal("derived key survived revoke")
	}
	if _, e := r.Read(context.Background(), VSCode); !errors.Is(e, ErrAuthorize) {
		t.Fatal("revoked key still usable")
	}
}
