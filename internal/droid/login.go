package droid

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Keychain items that hold the key of a sealed login file.
const (
	KeychainService = "Factory CLI"
	// KeychainAccountSecurityCLI keys auth.v2.loginkeychain (macOS).
	KeychainAccountSecurityCLI = "auth-encryption-key-security-cli"
	// KeychainAccountKeyring keys auth.v2.keyring (Windows and Linux).
	KeychainAccountKeyring = "auth-encryption-key"

	loginFileLimit = 64 << 10
	keyBytes       = 32
	ivBytes        = 16
	tagBytes       = 16
)

// Errors of Load.
var (
	// ErrNotInstalled: there is no Factory directory.
	ErrNotInstalled = errors.New("droid: not installed")
	// ErrNotLoggedIn: the directory holds no login file.
	ErrNotLoggedIn = errors.New("droid: not signed in")
	// ErrLoginUnreadable: login files exist but none could be opened.
	ErrLoginUnreadable = errors.New("droid: login unreadable")
	// ErrLoginExpired: the newest login's access token has expired.
	ErrLoginExpired = errors.New("droid: login expired")
	// ErrKeyUnavailable: the key of a login file cannot be read here.
	ErrKeyUnavailable = errors.New("droid: key unavailable")
)

// Login is the signed-in Droid account. String never shows the token.
type Login struct {
	AccessToken string
	Email       string
	Name        string
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

func (l Login) String() string { return "droid.Login{" + l.Email + "}" }

// Store reads the login of one Factory directory.
type Store struct {
	// Dir is the Factory directory, normally <home>/.factory.
	Dir string
	// Key returns the base64 key kept in the keychain item of account; nil
	// uses SystemKey.
	Key func(ctx context.Context, account string) (string, error)
}

// NewStore is the Store of home's Factory directory.
func NewStore(home string) Store { return Store{Dir: filepath.Join(home, ".factory")} }

// Installed reports whether the Factory directory exists.
func (s Store) Installed() bool {
	info, err := os.Stat(s.Dir)
	return err == nil && info.IsDir()
}

// LoginFiles are the paths that may hold the login.
func (s Store) LoginFiles() []string {
	out := make([]string, len(loginFiles))
	for i, f := range loginFiles {
		out[i] = filepath.Join(s.Dir, f.name)
	}
	return out
}

type loginFile struct {
	name    string
	account string // keychain account of the key, "" for keyFile
	keyFile string
}

var loginFiles = []loginFile{
	{name: "auth.v2.loginkeychain", account: KeychainAccountSecurityCLI},
	{name: "auth.v2.keyring", account: KeychainAccountKeyring},
	{name: "auth.v2.file", keyFile: "auth.v2.key"},
}

// Load opens every login file and returns the login issued last. An expired
// login comes back with ErrLoginExpired.
func (s Store) Load(ctx context.Context, now time.Time) (Login, error) {
	found := false
	var best *Login
	for _, f := range loginFiles {
		sealed, err := readLimited(filepath.Join(s.Dir, f.name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		found = true
		if err != nil {
			continue
		}
		key, err := s.loadKey(ctx, f)
		if ctx.Err() != nil {
			return Login{}, ctx.Err()
		}
		if err != nil {
			continue
		}
		plain, err := open(string(sealed), key)
		if err != nil {
			continue
		}
		login, err := parseLogin(plain)
		if err != nil {
			continue
		}
		if best == nil || login.IssuedAt.After(best.IssuedAt) {
			best = &login
		}
	}
	switch {
	case best != nil:
	case found:
		return Login{}, ErrLoginUnreadable
	case !s.Installed():
		return Login{}, ErrNotInstalled
	default:
		return Login{}, ErrNotLoggedIn
	}
	if !best.ExpiresAt.IsZero() && !now.Before(best.ExpiresAt) {
		return *best, ErrLoginExpired
	}
	return *best, nil
}

func (s Store) loadKey(ctx context.Context, f loginFile) ([]byte, error) {
	var text string
	if f.keyFile != "" {
		data, err := readLimited(filepath.Join(s.Dir, f.keyFile))
		if err != nil {
			return nil, err
		}
		text = string(data)
	} else {
		load := s.Key
		if load == nil {
			load = SystemKey
		}
		var err error
		if text, err = load(ctx, f.account); err != nil {
			return nil, err
		}
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(key) != keyBytes {
		return nil, ErrKeyUnavailable
	}
	return key, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, loginFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > loginFileLimit {
		return nil, ErrLoginUnreadable
	}
	return data, nil
}

// open unseals base64(iv):base64(tag):base64(ciphertext).
func open(sealed string, key []byte) ([]byte, error) {
	parts := strings.Split(strings.TrimSpace(sealed), ":")
	if len(parts) != 3 {
		return nil, ErrLoginUnreadable
	}
	var raw [3][]byte
	for i, p := range parts {
		b, err := base64.StdEncoding.DecodeString(p)
		if err != nil {
			return nil, ErrLoginUnreadable
		}
		raw[i] = b
	}
	iv, tag, ct := raw[0], raw[1], raw[2]
	if len(iv) != ivBytes || len(tag) != tagBytes {
		return nil, ErrLoginUnreadable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrLoginUnreadable
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, ivBytes)
	if err != nil {
		return nil, ErrLoginUnreadable
	}
	plain, err := gcm.Open(nil, iv, append(ct, tag...), nil)
	if err != nil {
		return nil, ErrLoginUnreadable
	}
	return plain, nil
}

// parseLogin reads the login JSON and the claims of its access token.
func parseLogin(plain []byte) (Login, error) {
	var doc struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(plain, &doc); err != nil || doc.AccessToken == "" {
		return Login{}, ErrLoginUnreadable
	}
	login := Login{AccessToken: doc.AccessToken}
	parts := strings.Split(doc.AccessToken, ".")
	if len(parts) != 3 {
		return login, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return login, nil
	}
	var claims struct {
		Email     string   `json:"email"`
		FirstName *string  `json:"first_name"`
		LastName  *string  `json:"last_name"`
		Exp       *float64 `json:"exp"`
		Iat       *float64 `json:"iat"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return login, nil
	}
	login.Email = strings.TrimSpace(claims.Email)
	var names []string
	for _, n := range []*string{claims.FirstName, claims.LastName} {
		if n != nil && strings.TrimSpace(*n) != "" {
			names = append(names, strings.TrimSpace(*n))
		}
	}
	login.Name = strings.Join(names, " ")
	if claims.Exp != nil {
		login.ExpiresAt = time.Unix(int64(*claims.Exp), 0).UTC()
	}
	if claims.Iat != nil {
		login.IssuedAt = time.Unix(int64(*claims.Iat), 0).UTC()
	}
	return login, nil
}
