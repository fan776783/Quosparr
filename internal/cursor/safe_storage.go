package cursor

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const safeStoragePrefix = "cursor-safe-storage:v1:"

func safeStorageCiphertext(value string) ([]byte, error) {
	if !strings.HasPrefix(value, safeStoragePrefix) {
		return nil, ErrEncryptedStorage
	}
	if len(value) > 16384 {
		return nil, ErrCredentials
	}
	b, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(value, safeStoragePrefix))
	if err != nil || len(b) < 3 {
		return nil, ErrCredentials
	}
	if string(b[:3]) != "v10" {
		return nil, ErrEncryptedStorage
	}
	if len(b) < 19 || (len(b)-3)%aes.BlockSize != 0 {
		return nil, ErrCredentials
	}
	return b[3:], nil
}

// Electron's macOS v10 format uses PBKDF2-HMAC-SHA1 (1003 iterations,
// "saltysalt", 128-bit key) and AES-CBC with a sixteen-space IV.
func deriveSafeStorageKey(password []byte) []byte {
	mac := hmac.New(sha1.New, password)
	mac.Write([]byte("saltysalt\x00\x00\x00\x01"))
	u := mac.Sum(nil)
	key := append([]byte(nil), u...)
	for i := 1; i < 1003; i++ {
		mac.Reset()
		mac.Write(u)
		clear(u)
		u = mac.Sum(nil)
		for j := range key {
			key[j] ^= u[j]
		}
	}
	clear(u)
	out := append([]byte(nil), key[:16]...)
	clear(key)
	return out
}

func decryptSafeStorage(sealed, key []byte, now time.Time) (string, error) {
	if len(key) != 16 || len(sealed) == 0 || len(sealed)%aes.BlockSize != 0 {
		return "", ErrCredentials
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", ErrCredentials
	}
	plain := make([]byte, len(sealed))
	defer clear(plain)
	cipher.NewCBCDecrypter(block, []byte("                ")).CryptBlocks(plain, sealed)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > aes.BlockSize {
		return "", ErrCredentials
	}
	for _, b := range plain[len(plain)-pad:] {
		if int(b) != pad {
			return "", ErrCredentials
		}
	}
	token := string(plain[:len(plain)-pad])
	if !validToken(token) {
		return "", ErrCredentials
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[2] == "" {
		return "", ErrCredentials
	}
	header, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return "", ErrCredentials
	}
	defer clear(header)
	var algorithm struct {
		Algorithm string `json:"alg"`
	}
	if json.Unmarshal(header, &algorithm) != nil || algorithm.Algorithm == "" || algorithm.Algorithm == "none" {
		return "", ErrCredentials
	}
	if signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[2]); err != nil || len(signature) == 0 {
		return "", ErrCredentials
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return "", ErrCredentials
	}
	defer clear(data)
	var claims struct {
		Subject string `json:"sub"`
		Expires int64  `json:"exp"`
	}
	if json.Unmarshal(data, &claims) != nil || claims.Subject == "" || claims.Expires <= 0 {
		return "", ErrCredentials
	}
	// Structural checks only: the server remains the authority for validity.
	if claims.Expires <= now.Unix()+60 {
		return "", ErrUnauthorized
	}
	return token, nil
}
