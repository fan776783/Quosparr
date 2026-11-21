package droid

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)

func testToken(t *testing.T, email string, iat, exp time.Time) string {
	t.Helper()
	claims, _ := json.Marshal(map[string]any{
		"email": email, "first_name": "Ada", "last_name": "Lovelace",
		"iat": iat.Unix(), "exp": exp.Unix(),
	})
	return "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
}

func seal(t *testing.T, key []byte, plain string) string {
	t.Helper()
	iv := make([]byte, ivBytes)
	_, _ = rand.Read(iv)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCMWithNonceSize(block, ivBytes)
	out := gcm.Seal(nil, iv, []byte(plain), nil)
	ct, tag := out[:len(out)-tagBytes], out[len(out)-tagBytes:]
	enc := base64.StdEncoding.EncodeToString
	return enc(iv) + ":" + enc(tag) + ":" + enc(ct)
}

func newKey() []byte {
	k := make([]byte, keyBytes)
	_, _ = rand.Read(k)
	return k
}

func writeLogin(t *testing.T, dir, name string, key []byte, token string) {
	t.Helper()
	doc, _ := json.Marshal(map[string]any{"access_token": token, "refresh_token": "synthetic-refresh"})
	if err := os.WriteFile(filepath.Join(dir, name), []byte(seal(t, key, string(doc))), 0o600); err != nil {
		t.Fatal(err)
	}
}

func noKeychain(context.Context, string) (string, error) { return "", ErrKeyUnavailable }

func TestLoadPicksNewestLogin(t *testing.T) {
	dir := t.TempDir()
	fileKey, chainKey := newKey(), newKey()
	_ = os.WriteFile(filepath.Join(dir, "auth.v2.key"), []byte(base64.StdEncoding.EncodeToString(fileKey)+"\n"), 0o600)
	writeLogin(t, dir, "auth.v2.file", fileKey, testToken(t, "old@example.com", testNow.Add(-48*time.Hour), testNow.Add(-24*time.Hour)))
	writeLogin(t, dir, "auth.v2.loginkeychain", chainKey, testToken(t, "new@example.com", testNow.Add(-time.Hour), testNow.Add(23*time.Hour)))
	var asked []string
	s := Store{Dir: dir, Key: func(_ context.Context, account string) (string, error) {
		asked = append(asked, account)
		if account == KeychainAccountSecurityCLI {
			return base64.StdEncoding.EncodeToString(chainKey), nil
		}
		return "", ErrKeyUnavailable
	}}
	login, err := s.Load(context.Background(), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if login.Email != "new@example.com" || login.Name != "Ada Lovelace" || !login.ExpiresAt.Equal(testNow.Add(23*time.Hour).Truncate(time.Second)) {
		t.Fatalf("login = %+v", login)
	}
	if len(asked) != 1 || asked[0] != KeychainAccountSecurityCLI {
		t.Fatalf("keychain accounts asked = %v", asked)
	}
	if strings.Contains(login.String(), login.AccessToken) {
		t.Fatal("String shows the token")
	}
}

func TestLoadExpiredKeepsIdentity(t *testing.T) {
	dir := t.TempDir()
	key := newKey()
	_ = os.WriteFile(filepath.Join(dir, "auth.v2.key"), []byte(base64.StdEncoding.EncodeToString(key)), 0o600)
	writeLogin(t, dir, "auth.v2.file", key, testToken(t, "a@example.com", testNow.Add(-25*time.Hour), testNow.Add(-time.Hour)))
	login, err := Store{Dir: dir, Key: noKeychain}.Load(context.Background(), testNow)
	if !errors.Is(err, ErrLoginExpired) || login.Email != "a@example.com" {
		t.Fatalf("login %+v err %v", login, err)
	}
}

func TestLoadStates(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := (Store{Dir: missing, Key: noKeychain}).Load(context.Background(), testNow); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("missing dir: %v", err)
	}
	empty := t.TempDir()
	if _, err := (Store{Dir: empty, Key: noKeychain}).Load(context.Background(), testNow); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("empty dir: %v", err)
	}
	broken := t.TempDir()
	writeLogin(t, broken, "auth.v2.loginkeychain", newKey(), "x")
	_ = os.WriteFile(filepath.Join(broken, "auth.v2.key"), []byte(base64.StdEncoding.EncodeToString(newKey())), 0o600)
	writeLogin(t, broken, "auth.v2.file", newKey(), "x")
	if _, err := (Store{Dir: broken, Key: noKeychain}).Load(context.Background(), testNow); !errors.Is(err, ErrLoginUnreadable) {
		t.Fatalf("wrong keys: %v", err)
	}
}

const limitsFixture = `{
 "extraUsageAllowed": true, "usesTokenRateLimitsBilling": true, "hasActiveSubscription": true,
 "limits": {
  "standard": {
   "fiveHour": {"usedPercent": 23, "windowEnd": "2026-11-21T06:05:10.671Z", "secondsRemaining": 16200},
   "weekly": {"usedPercent": 51.5, "windowEnd": "2026-11-26T13:48:57.902Z", "secondsRemaining": 476028},
   "monthly": {"usedPercent": 56, "windowEnd": "2026-11-28T15:36:56.247Z", "secondsRemaining": 655306}
  },
  "core": {
   "fiveHour": {"usedPercent": 0, "windowEnd": null, "secondsRemaining": null},
   "weekly": {"usedPercent": 75, "windowEnd": "2026-11-25T05:28:31.027Z", "secondsRemaining": 359601}
  }
 },
 "overagePreference": "droidCore", "canManageOverage": true, "extraUsageBalanceCents": 1250
}`

func TestParseLimits(t *testing.T) {
	l, err := ParseLimits([]byte(limitsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if l.Standard.FiveHour.UsedPercent != 23 || l.Standard.Weekly.UsedPercent != 51.5 || l.Standard.Monthly.EndsAt.IsZero() {
		t.Fatalf("standard = %+v", l.Standard)
	}
	if l.Core == nil || !l.Core.FiveHour.EndsAt.IsZero() || l.Core.Weekly.UsedPercent != 75 || l.Core.Monthly != nil {
		t.Fatalf("core = %+v", l.Core)
	}
	if l.ExtraUsageBalanceCents == nil || *l.ExtraUsageBalanceCents != 1250 {
		t.Fatalf("extra = %v", l.ExtraUsageBalanceCents)
	}
	if _, err := ParseLimits([]byte(`{"hasActiveSubscription":false,"limits":{}}`)); !errors.Is(err, ErrNoSubscription) {
		t.Fatalf("no subscription: %v", err)
	}
	for _, bad := range []string{`[]`, `{"hasActiveSubscription":true}`, `{"limits":{"standard":{}}}`, `{"limits":{"standard":{"weekly":{"usedPercent":1,"windowEnd":"soon"}}}}`} {
		if _, err := ParseLimits([]byte(bad)); !errors.Is(err, ErrBadResponse) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
}

func TestFetch(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Header.Get("Cookie") != "" {
			t.Error("cookie sent")
		}
		switch r.URL.Path {
		case "/api/billing/limits":
			_, _ = w.Write([]byte(limitsFixture))
		case "/api/app/auth/me":
			_, _ = w.Write([]byte(`{"organization":{"name":"x","subscription":{"factoryTier":"team_annual","orbSubscription":{"customer":{"portal_url":"https://example.invalid"}}}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), Origin: srv.URL}
	l, err := c.Fetch(context.Background(), Login{AccessToken: "synthetic"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if l.Plan != "team_annual" || PlanName(l.Plan) != "Team" || !l.UpdatedAt.Equal(testNow) {
		t.Fatalf("limits = %+v", l)
	}
	if len(auth) != 2 || auth[0] != "Bearer synthetic" {
		t.Fatalf("auth = %v", auth)
	}
}

func TestFetchErrors(t *testing.T) {
	for status, want := range map[int]error{401: ErrUnauthorized, 403: ErrUnauthorized, 429: ErrRateLimited, 500: ErrRequestFailed} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		_, err := Client{HTTP: srv.Client(), Origin: srv.URL}.Fetch(context.Background(), Login{AccessToken: "synthetic"}, testNow)
		srv.Close()
		if !errors.Is(err, want) {
			t.Fatalf("HTTP %d: %v", status, err)
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.invalid/", http.StatusFound)
	}))
	defer redirect.Close()
	if _, err := (Client{HTTP: redirect.Client(), Origin: redirect.URL}).Fetch(context.Background(), Login{AccessToken: "synthetic"}, testNow); !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("redirect: %v", err)
	}
}

func TestPlanName(t *testing.T) {
	for in, want := range map[string]string{"team_annual": "Team", "pro_monthly": "Pro", "max": "Max", "": "", "enterprise-trial": "Enterprise Trial"} {
		if got := PlanName(in); got != want {
			t.Errorf("PlanName(%q) = %q, want %q", in, got, want)
		}
	}
}
