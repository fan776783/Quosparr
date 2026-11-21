package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/devin"
	"github.com/licoy/quosparr/internal/droid"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

var quotaOnlyNow = time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)

func quotaOnlyPrefs() store.Preferences {
	p := store.DefaultPreferences()
	p.DroidMonitoringEnabled, p.DevinMonitoringEnabled = true, true
	return p
}

// writeDroidLogin seals a synthetic login into dir/auth.v2.file.
func writeDroidLogin(t *testing.T, dir, email string, exp time.Time) {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	claims, _ := json.Marshal(map[string]any{"email": email, "first_name": "Ada", "iat": quotaOnlyNow.Add(-time.Hour).Unix(), "exp": exp.Unix()})
	doc, _ := json.Marshal(map[string]string{"access_token": "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig", "refresh_token": "synthetic"})
	iv := make([]byte, 16)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCMWithNonceSize(block, 16)
	sealed := gcm.Seal(nil, iv, doc, nil)
	enc := base64.StdEncoding.EncodeToString
	text := enc(iv) + ":" + enc(sealed[len(sealed)-16:]) + ":" + enc(sealed[:len(sealed)-16])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "auth.v2.key"), []byte(enc(key)), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "auth.v2.file"), []byte(text), 0o600)
}

func droidTestBridge(t *testing.T, dir string, handler http.HandlerFunc) *quotaOnlyBridge {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	noKey := func(context.Context, string) (string, error) { return "", droid.ErrKeyUnavailable }
	src := &droidSource{store: droid.Store{Dir: dir, Key: noKey}, client: droid.Client{HTTP: srv.Client(), Origin: srv.URL}}
	b := newQuotaOnlyBridge(store.ProviderDroid, src, quotaOnlyPrefs(), []string{"en"})
	b.now = func() time.Time { return quotaOnlyNow }
	return b
}

const droidLimitsBody = `{"hasActiveSubscription":true,"extraUsageBalanceCents":1250,"limits":{
 "standard":{"fiveHour":{"usedPercent":23.4,"windowEnd":"2026-11-21T10:00:00Z"},"weekly":{"usedPercent":51,"windowEnd":"2026-11-26T00:00:00Z"}},
 "core":{"weekly":{"usedPercent":75,"windowEnd":"2026-11-25T00:00:00Z"}}}}`

func TestDroidBridgeReadsQuota(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".factory")
	writeDroidLogin(t, dir, "ada@example.com", quotaOnlyNow.Add(time.Hour))
	b := droidTestBridge(t, dir, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/app/auth/me" {
			_, _ = w.Write([]byte(`{"organization":{"subscription":{"factoryTier":"pro_monthly"}}}`))
			return
		}
		_, _ = w.Write([]byte(droidLimitsBody))
	})
	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st := b.State(quotaOnlyNow)
	if st.Availability != runway.QuotaOnlyReady || st.Name != "Ada" || st.Email != "ada@example.com" || st.Plan != "Pro" {
		t.Fatalf("state %+v", st)
	}
	if len(st.Meters) != 3 || st.Meters[0].Title != "Standard · 5-hour" || st.Meters[0].UsedPercent != 23 ||
		st.Meters[0].Source != runway.SourceStandard || st.Meters[2].Source != runway.SourceModelSpecific || st.Meters[2].Title != "Core · Weekly" {
		t.Fatalf("meters %+v", st.Meters)
	}
	if len(st.Details) != 2 || st.Details[1].Value != "$12.50" {
		t.Fatalf("details %+v", st.Details)
	}
}

func TestDroidBridgeStates(t *testing.T) {
	calls := atomic.Int32{}
	status := atomic.Int32{}
	status.Store(http.StatusOK)
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if code := int(status.Load()); code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		_, _ = w.Write([]byte(droidLimitsBody))
	}
	missing := droidTestBridge(t, filepath.Join(t.TempDir(), ".factory"), handler)
	_ = missing.Refresh(context.Background(), false)
	if st := missing.State(quotaOnlyNow); st.Availability != runway.QuotaOnlyNotInstalled {
		t.Fatalf("missing: %+v", st)
	}

	dir := filepath.Join(t.TempDir(), ".factory")
	writeDroidLogin(t, dir, "ada@example.com", quotaOnlyNow.Add(time.Hour))
	b := droidTestBridge(t, dir, handler)
	_ = b.Refresh(context.Background(), false)
	status.Store(http.StatusTooManyRequests)
	_ = b.Refresh(context.Background(), false)
	st := b.State(quotaOnlyNow)
	if st.Availability != runway.QuotaOnlyRateLimited || len(st.Meters) == 0 {
		t.Fatalf("rate limited keeps the reading: %+v", st)
	}
	before := calls.Load()
	_ = b.Refresh(context.Background(), false)
	if calls.Load() != before {
		t.Fatal("automatic refresh ignored the backoff")
	}
	status.Store(http.StatusUnauthorized)
	_ = b.Refresh(context.Background(), true)
	if st := b.State(quotaOnlyNow); st.Availability != runway.QuotaOnlyReauthRequired || calls.Load() == before {
		t.Fatalf("manual refresh: %+v", st)
	}

	expired := filepath.Join(t.TempDir(), ".factory")
	writeDroidLogin(t, expired, "old@example.com", quotaOnlyNow.Add(-time.Minute))
	e := droidTestBridge(t, expired, handler)
	_ = e.Refresh(context.Background(), false)
	if st := e.State(quotaOnlyNow); st.Availability != runway.QuotaOnlyReauthRequired || st.Email != "old@example.com" ||
		st.LastError != l10n.New(l10n.English).Text(l10n.KeyDroidLoginExpired) {
		t.Fatalf("expired: %+v", st)
	}
}

func TestDevinBridgeReadsQuotaAndRelabels(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"userStatus":{"name":"Ada","email":"ada@example.com","planStatus":{"planInfo":{"planName":"Pro","billingStrategy":"BILLING_STRATEGY_QUOTA"},
"dailyQuotaRemainingPercent":60,"weeklyQuotaRemainingPercent":81,"dailyQuotaResetAtUnix":"1795248000","weeklyQuotaResetAtUnix":"1795334400"}}}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	creds := "windsurf_api_key = \"devin-synthetic\"\napi_server_url = \"" + srv.URL + "\"\n"
	if err := os.WriteFile(devin.CredentialsPath(dir), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &devinSource{dir: dir, client: devin.Client{HTTP: srv.Client(), Version: "test"}}
	b := newQuotaOnlyBridge(store.ProviderDevin, src, quotaOnlyPrefs(), []string{"en"})
	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st := b.State(quotaOnlyNow)
	if st.Availability != runway.QuotaOnlyReady || st.Plan != "Pro" || len(st.Meters) != 2 ||
		st.Meters[0].Title != "Daily" || st.Meters[0].RemainingPercent != 60 || st.Meters[1].WindowMinutes != runway.WeeklyMinutes {
		t.Fatalf("state %+v", st)
	}
	p := quotaOnlyPrefs()
	p.Language = store.LanguageSimplifiedChinese
	b.setPreferences(p, nil)
	if got := b.State(quotaOnlyNow).Meters[0].Title; got != l10n.New(l10n.SimplifiedChinese).Text(l10n.KeyHeatmapDaily) {
		t.Fatalf("relabelled title %q", got)
	}
	if strings.Contains(b.State(quotaOnlyNow).LastError, "devin-synthetic") {
		t.Fatal("key in state")
	}
	_ = os.Remove(devin.CredentialsPath(dir))
	_ = b.Refresh(context.Background(), false)
	if st := b.State(quotaOnlyNow); st.Availability != runway.QuotaOnlyNotLoggedIn || len(st.Meters) != 0 {
		t.Fatalf("signed out: %+v", st)
	}
}

func TestQuotaOnlyBridgeMonitoringSwitch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".factory")
	writeDroidLogin(t, dir, "ada@example.com", quotaOnlyNow.Add(time.Hour))
	var calls atomic.Int32
	b := droidTestBridge(t, dir, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(droidLimitsBody))
	})
	_ = b.Refresh(context.Background(), true)
	off := quotaOnlyPrefs()
	off.DroidMonitoringEnabled = false
	b.setPreferences(off, nil)
	if st := b.State(quotaOnlyNow); st.Availability != runway.QuotaOnlyDisabled || len(st.Meters) != 0 || st.Email != "" {
		t.Fatalf("disabled state %+v", st)
	}
	before := calls.Load()
	_ = b.Refresh(context.Background(), true)
	if calls.Load() != before {
		t.Fatal("disabled bridge read the quota")
	}
	b.setPreferences(quotaOnlyPrefs(), nil)
	if st := b.State(quotaOnlyNow); st.Availability != runway.QuotaOnlyLoading || len(st.Meters) != 0 {
		t.Fatalf("re-enabled state kept the old reading: %+v", st)
	}
}
