package copilot

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/sqliteruntime"
)

const CLI = "cli"
const VSCode = "vscode"
const VSCodeInsiders = "vscode-insiders"
const secretKey = `secret://{"extensionId":"vscode.github-authentication","key":"github.auth"}`

type Auth struct {
	ID, Login, Host string
	Token           string `json:"-"`
}

func (Auth) String() string   { return "copilot credentials (redacted)" }
func (Auth) GoString() string { return "copilot credentials (redacted)" }

func validToken(s string) bool {
	if len(s) == 0 || len(s) > 16384 {
		return false
	}
	for _, c := range s {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}

// Reader never displays OS authorization prompts from Read. Authorize is
// called only from the explicit foreground action. Clear revokes cached keys.
type Reader struct {
	Home, CLIDir, VSCodeDir string
	mu                      sync.Mutex
	keys                    map[string][]byte
	tokens                  map[string]string
	password                func(context.Context, string, string) ([]byte, error)
}

func NewReader(home string) *Reader { return &Reader{Home: home, password: readPassword} }
func (r *Reader) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.keys {
		clear(k)
	}
	r.keys = nil
	r.tokens = nil
}
func (r *Reader) Read(ctx context.Context, source string) ([]Auth, error) {
	return r.read(ctx, source, false)
}
func (r *Reader) Authorize(ctx context.Context, source string) ([]Auth, error) {
	return r.read(ctx, source, true)
}
func (r *Reader) read(ctx context.Context, source string, authorize bool) ([]Auth, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if source == CLI {
		return r.readCLI(ctx, authorize)
	}
	if source != VSCode && source != VSCodeInsiders {
		return nil, ErrCredentials
	}
	dir := r.VSCodeDir
	name, service := "Code", "Code Safe Storage"
	if source == VSCodeInsiders {
		name, service = "Code - Insiders", "Code - Insiders Safe Storage"
	}
	if dir == "" {
		switch runtime.GOOS {
		case "darwin":
			dir = filepath.Join(r.Home, "Library", "Application Support", name)
		case "windows":
			dir = filepath.Join(r.Home, "AppData", "Roaming", name)
		default:
			dir = filepath.Join(r.Home, ".config", name)
		}
	}
	sealed, err := readVSCode(ctx, filepath.Join(dir, "User", "globalStorage", "state.vscdb"))
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "darwin" {
		return nil, ErrUnsupported
	}
	key := r.keys[source]
	if len(key) == 0 {
		if !authorize {
			return nil, ErrAuthorize
		}
		password, err := r.password(ctx, service, "")
		defer clear(password)
		if err != nil {
			return nil, err
		}
		key, err = pbkdf2.Key(sha1.New, string(password), []byte("saltysalt"), 1003, 16)
		if err != nil {
			return nil, ErrCredentials
		}
		if r.keys == nil {
			r.keys = map[string][]byte{}
		}
		r.keys[source] = key
	}
	plain, err := decrypt(sealed, key)
	if err != nil {
		clear(key)
		delete(r.keys, source)
		return nil, ErrAuthorize
	}
	defer clear(plain)
	return parseSessions(plain)
}

func readVSCode(ctx context.Context, path string) ([]byte, error) {
	if err := regularFile(path, 0); err != nil {
		return nil, err
	}
	if sqliteruntime.Load() != nil {
		return nil, ErrCredentials
	}
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{ReadOnly: true, BusyTimeout: time.Second})
	if err != nil {
		return nil, ErrCredentials
	}
	defer db.Close()
	rows, err := db.Query(ctx, `SELECT CASE WHEN length(value) <= 1048576 THEN value ELSE NULL END FROM ItemTable WHERE key = ?`, secretKey)
	if err != nil {
		return nil, ErrCredentials
	}
	if len(rows.Values) == 0 {
		return nil, ErrLogin
	}
	if len(rows.Values) != 1 || len(rows.Values[0]) != 1 {
		return nil, ErrCredentials
	}
	var data []byte
	switch v := rows.Values[0][0].(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return nil, ErrCredentials
	}
	var envelope struct {
		Type string `json:"type"`
		Data []int  `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Type != "Buffer" || len(envelope.Data) > 1<<20 {
		return nil, ErrCredentials
	}
	out := make([]byte, len(envelope.Data))
	for i, v := range envelope.Data {
		if v < 0 || v > 255 {
			return nil, ErrCredentials
		}
		out[i] = byte(v)
	}
	if len(out) < 19 || string(out[:3]) != "v10" || (len(out)-3)%aes.BlockSize != 0 {
		return nil, ErrUnsupported
	}
	return out[3:], nil
}

func decrypt(data, key []byte) ([]byte, error) {
	if len(key) != 16 || len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, ErrCredentials
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCredentials
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(b, []byte("                ")).CryptBlocks(plain, data)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > aes.BlockSize {
		clear(plain)
		return nil, ErrCredentials
	}
	for _, v := range plain[len(plain)-pad:] {
		if int(v) != pad {
			clear(plain)
			return nil, ErrCredentials
		}
	}
	return plain[:len(plain)-pad], nil
}

func parseSessions(data []byte) ([]Auth, error) {
	var sessions []struct {
		ID          string                     `json:"id"`
		AccessToken string                     `json:"accessToken"`
		Account     struct{ ID, Label string } `json:"account"`
	}
	if json.Unmarshal(data, &sessions) != nil {
		return nil, ErrCredentials
	}
	var out []Auth
	seen := map[string]bool{}
	for _, s := range sessions {
		if !validToken(s.AccessToken) || s.Account.ID == "" || s.Account.Label == "" {
			continue
		}
		if !seen[s.Account.ID] {
			out = append(out, Auth{ID: s.Account.ID, Login: s.Account.Label, Host: "https://github.com", Token: s.AccessToken})
			seen[s.Account.ID] = true
		}
	}
	if len(out) == 0 {
		return nil, ErrLogin
	}
	return out, nil
}

func regularFile(path string, maxBytes int64) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrLogin
	}
	if err != nil || !info.Mode().IsRegular() || (maxBytes > 0 && info.Size() > maxBytes) {
		return ErrCredentials
	}
	return nil
}

func (r *Reader) readCLI(ctx context.Context, authorize bool) ([]Auth, error) {
	dir := r.CLIDir
	if dir == "" {
		dir = filepath.Join(r.Home, ".copilot")
	}
	path := filepath.Join(dir, "config.json")
	if err := regularFile(path, 1<<20); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrCredentials
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, ErrCredentials
	}
	defer clear(data)
	return r.parseCLI(ctx, stripComments(data), authorize)
}

// JSONC comments and trailing commas are allowed in current CLI config files.
func stripComments(data []byte) []byte {
	out := append([]byte(nil), data...)
	quoted, escape := false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if quoted {
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c == '/' && i+1 < len(out) && out[i+1] == '/' {
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
		} else if c == '/' && i+1 < len(out) && out[i+1] == '*' {
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i < len(out) {
				if i+1 < len(out) && out[i] == '*' && out[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i++
					break
				}
				out[i] = ' '
				i++
			}
		}
	}
	quoted, escape = false, false
	for i, c := range out {
		if quoted {
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(out) && strings.ContainsRune(" \n\r\t", rune(out[j])) {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				out[i] = ' '
			}
		}
	}
	return out
}
