package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/cachehit"
	"github.com/licoy/quosparr/internal/devin"
	"github.com/licoy/quosparr/internal/droid"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/sqliteruntime"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
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
	if len(st.Meters) != 2 || st.Meters[0].Title != "Standard · 5-hour" || st.Meters[0].UsedPercent != 23 ||
		st.Meters[0].Source != runway.SourceStandard || st.Meters[1].Title != "Standard · Weekly" || st.Meters[1].Source != runway.SourceStandard {
		t.Fatalf("meters %+v", st.Meters)
	}
	if !st.Meters[0].ResetsAt.Equal(time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)) ||
		!st.Meters[1].ResetsAt.Equal(time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("resets %+v", st.Meters)
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

func testQuotaOnlyPricing(t *testing.T, kimiInput string) pricing.Snapshot {
	t.Helper()
	d := func(s string) *decimal.Decimal { v := decimal.MustParse(s); return &v }
	snap, err := pricing.NewSnapshot([]pricing.Model{
		{ProviderID: "anthropic", ID: "claude-opus-5-5", Rate: pricing.Rate{Input: d("5"), Output: d("25"), CacheRead: d("0.5"), CacheWrite: d("6.25")}},
		{ProviderID: "moonshotai", ID: "kimi-k3", Rate: pricing.Rate{Input: d(kimiInput), Output: d("4")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestQuotaOnlyCostWindow(t *testing.T) {
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.Local)
	week := 7 * 24 * time.Hour
	futureReset := now.Add(48 * time.Hour)
	pastReset := now.Add(-time.Hour)

	droidDetail := &droid.Limits{Standard: droid.Pool{Weekly: &droid.Window{UsedPercent: 51, EndsAt: futureReset}}}
	devinDetail := &devin.Status{Weekly: &devin.Window{RemainingPercent: 80, ResetsAt: futureReset}}

	for _, tc := range []struct {
		provider store.Provider
		detail   any
	}{
		{store.ProviderDroid, droidDetail},
		{store.ProviderDevin, devinDetail},
	} {
		cur := quotaOnlyCostWindow(tc.provider, store.CostRangeCurrent, tc.detail, now)
		if !cur.Start.Equal(futureReset.Add(-week)) || !cur.End.Equal(now) {
			t.Fatalf("%s current = %+v", tc.provider, cur)
		}
		prev := quotaOnlyCostWindow(tc.provider, store.CostRangePrevious, tc.detail, now)
		if !prev.End.Equal(cur.Start) || !prev.Start.Equal(cur.Start.Add(-week)) {
			t.Fatalf("%s previous = %+v", tc.provider, prev)
		}
		today := quotaOnlyCostWindow(tc.provider, store.CostRangeToday, tc.detail, now)
		if today != usage.Today(now, time.Local).Window {
			t.Fatalf("%s today = %+v", tc.provider, today)
		}
		month := quotaOnlyCostWindow(tc.provider, store.CostRangeThisMonth, tc.detail, now)
		if month != usage.ThisMonth(now, time.Local).Window {
			t.Fatalf("%s month = %+v", tc.provider, month)
		}
	}

	expiredDroid := &droid.Limits{Standard: droid.Pool{Weekly: &droid.Window{EndsAt: pastReset}}}
	expiredDevin := &devin.Status{Weekly: &devin.Window{ResetsAt: pastReset}}
	for _, detail := range []any{nil, expiredDroid, expiredDevin} {
		cur := quotaOnlyCostWindow(store.ProviderDroid, store.CostRangeCurrent, detail, now)
		if !cur.Start.Equal(now.Add(-week)) || !cur.End.Equal(now) {
			t.Fatalf("fallback current %v = %+v", detail, cur)
		}
		prev := quotaOnlyCostWindow(store.ProviderDevin, store.CostRangePrevious, detail, now)
		if !prev.Start.Equal(now.Add(-2*week)) || !prev.End.Equal(now.Add(-week)) {
			t.Fatalf("fallback previous %v = %+v", detail, prev)
		}
	}
}

func TestQuotaOnlyCostLines(t *testing.T) {
	zh := l10n.New(l10n.SimplifiedChinese)
	reset := quotaOnlyNow.Add(2 * time.Hour)
	standard5h := runway.Meter{Title: "Standard · 5小时", Source: runway.SourceStandard, WindowMinutes: runway.FiveHourMinutes, UsedPercent: 25, ResetsAt: reset}
	standard7dZero := runway.Meter{Title: "Standard · 每周", Source: runway.SourceStandard, WindowMinutes: runway.WeeklyMinutes, UsedPercent: 0, ResetsAt: reset}
	standard30d := runway.Meter{Title: "Standard · 每月", Source: runway.SourceStandard, WindowMinutes: runway.MonthlyMinutes, UsedPercent: 50, ResetsAt: reset}
	core7d := runway.Meter{Title: "Core · 每周", Source: runway.SourceModelSpecific, WindowMinutes: runway.WeeklyMinutes, UsedPercent: 80, ResetsAt: reset}
	unstarted := runway.Meter{Title: "Standard · 未开始", Source: runway.SourceStandard, WindowMinutes: runway.WeeklyMinutes, UsedPercent: 10}

	cases := []struct {
		name   string
		meters []runway.Meter
		costs  []runway.QuotaOnlyWindowCost
		want   []runway.DetailLine
	}{
		{
			name:   "estimated total",
			meters: []runway.Meter{standard5h},
			costs:  []runway.QuotaOnlyWindowCost{{Source: runway.SourceStandard, WindowMinutes: runway.FiveHourMinutes, USD: decimal.MustParse("12.5"), Priced: true}},
			want:   []runway.DetailLine{{Title: "Standard · 5小时", Value: "$12.50 / $50.00 · 估算"}},
		},
		{
			name:   "estimated total with partial",
			meters: []runway.Meter{standard5h},
			costs:  []runway.QuotaOnlyWindowCost{{Source: runway.SourceStandard, WindowMinutes: runway.FiveHourMinutes, USD: decimal.MustParse("12.5"), Priced: true, Partial: true}},
			want:   []runway.DetailLine{{Title: "Standard · 5小时", Value: "$12.50 / $50.00 · 估算 · " + zh.Pricing("partial")}},
		},
		{
			name:   "used percent zero",
			meters: []runway.Meter{standard7dZero},
			costs:  []runway.QuotaOnlyWindowCost{{Source: runway.SourceStandard, WindowMinutes: runway.WeeklyMinutes, USD: decimal.MustParse("3.45"), Priced: true}},
			want:   []runway.DetailLine{{Title: "Standard · 每周 · " + zh.Text(l10n.KeyUsed), Value: "$3.45"}},
		},
		{
			name:   "all unpriced never shows 0/0",
			meters: []runway.Meter{standard30d},
			costs:  []runway.QuotaOnlyWindowCost{{Source: runway.SourceStandard, WindowMinutes: runway.MonthlyMinutes, Priced: false, Partial: true}},
			want:   []runway.DetailLine{{Title: "Standard · 每月 · " + zh.Text(l10n.KeyUsed), Value: zh.Pricing("unpriced")}},
		},
		{
			name:   "zero usage never shows 0/0",
			meters: []runway.Meter{standard30d},
			costs:  []runway.QuotaOnlyWindowCost{{Source: runway.SourceStandard, WindowMinutes: runway.MonthlyMinutes, Priced: false, Partial: false}},
			want:   []runway.DetailLine{{Title: "Standard · 每月 · " + zh.Text(l10n.KeyUsed), Value: "$0.00"}},
		},
		{
			name:   "skips droid core and unstarted windows",
			meters: []runway.Meter{standard5h, core7d, unstarted},
			costs: []runway.QuotaOnlyWindowCost{
				{Source: runway.SourceStandard, WindowMinutes: runway.FiveHourMinutes, USD: decimal.MustParse("12.5"), Priced: true},
				{Source: runway.SourceModelSpecific, WindowMinutes: runway.WeeklyMinutes, USD: decimal.MustParse("99"), Priced: true},
			},
			want: []runway.DetailLine{{Title: "Standard · 5小时", Value: "$12.50 / $50.00 · 估算"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := quotaOnlyCostLines(zh, tc.meters, tc.costs)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (%+v)", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDroidBridgeWindowCostsAndErrorPreservation(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".factory")
	writeDroidLogin(t, dir, "ada@example.com", quotaOnlyNow.Add(time.Hour))
	sessionsDir := filepath.Join(dir, "sessions")
	projDir := filepath.Join(sessionsDir, "my-app")
	if err := os.MkdirAll(projDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSess := func(name string, age time.Duration) {
		p := filepath.Join(projDir, name+".settings.json")
		if err := os.WriteFile(p, []byte(`{"model":"kimi-k3","tokenUsage":{"inputTokens":1000000,"outputTokens":100000}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := quotaOnlyNow.Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// Standard.FiveHour ends at 2026-11-21T10:00Z -> start 05:00Z (3h before quotaOnlyNow 08:00Z)
	// Standard.Weekly ends at 2026-11-26T00:00Z -> start 2026-11-19T00:00Z (~2d8h before quotaOnlyNow)
	// Standard.Monthly ends at 2026-12-10T00:00Z -> start 2026-11-10T00:00Z (~11d8h before quotaOnlyNow)
	writeSess("in-5h", 2*time.Hour)     // counted in 5h, 7d, 30d ($1.40 each)
	writeSess("in-7d", 24*time.Hour)    // counted in 7d, 30d ($1.40)
	writeSess("in-30d", 5*24*time.Hour) // counted in 30d ($1.40)

	limitsWithMonthly := `{"hasActiveSubscription":true,"limits":{
		"standard":{
			"fiveHour":{"usedPercent":20,"windowEnd":"2026-11-21T10:00:00Z"},
			"weekly":{"usedPercent":40,"windowEnd":"2026-11-26T00:00:00Z"},
			"monthly":{"usedPercent":50,"windowEnd":"2026-12-10T00:00:00Z"}
		},
		"core":{"weekly":{"usedPercent":75,"windowEnd":"2026-11-25T00:00:00Z"}}
	}}`
	b := droidTestBridge(t, dir, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(limitsWithMonthly))
	})
	b.sessionsDir = sessionsDir
	snap := testQuotaOnlyPricing(t, "1")
	b.pricingSnapshot = func() pricing.Snapshot { return snap }

	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st := b.State(quotaOnlyNow)
	if len(st.WindowCosts) != 3 {
		t.Fatalf("windowCosts = %+v, want 3 standard windows (no core)", st.WindowCosts)
	}
	wantByWin := map[int]string{
		runway.FiveHourMinutes: "1.4",
		runway.WeeklyMinutes:   "2.8",
		runway.MonthlyMinutes:  "4.2",
	}
	for _, wc := range st.WindowCosts {
		if wc.Source != runway.SourceStandard || !wc.Priced || wc.USD.String() != wantByWin[wc.WindowMinutes] {
			t.Fatalf("unexpected windowCost %+v, want %s", wc, wantByWin[wc.WindowMinutes])
		}
	}

	// Scan error preserves the previous WindowCosts.
	b.eventsScanner = func(context.Context, usage.Window, time.Time) ([]usage.PricedEvent, string, error) {
		return nil, "", errors.New("boom")
	}
	if err := b.RefreshLocal(context.Background()); err == nil {
		t.Fatal("expected error from failing eventsScanner")
	}
	if got := b.State(quotaOnlyNow).WindowCosts; len(got) != 3 || got[0].USD.String() != "1.4" {
		t.Fatalf("previous windowCosts lost on error: %+v", got)
	}

	// Disabling ShowsCostSummary clears WindowCosts without canceling quota state.
	p := quotaOnlyPrefs()
	p.ShowsCostSummary = false
	b.setPreferences(p, []string{"en"})
	stAfter := b.State(quotaOnlyNow)
	if len(stAfter.WindowCosts) != 0 || stAfter.Availability != runway.QuotaOnlyReady {
		t.Fatalf("state after ShowsCostSummary=false: %+v", stAfter)
	}
	if err := b.RefreshLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDevinBridgeCostCacheAndDetailQuery(t *testing.T) {
	pinQuotaOnlyLocalUTC(t)
	if err := sqliteruntime.Load(); err != nil {
		t.Fatal(err)
	}
	// Daily reset 12h ahead, weekly reset 5d ahead relative to quotaOnlyNow.
	dailyReset := quotaOnlyNow.Add(12 * time.Hour).Unix()
	weeklyReset := quotaOnlyNow.Add(5 * 24 * time.Hour).Unix()
	statusJSON := `{"userStatus":{"name":"Ada","email":"ada@example.com","planStatus":{"planInfo":{"planName":"Pro","billingStrategy":"BILLING_STRATEGY_QUOTA"},
"dailyQuotaRemainingPercent":60,"weeklyQuotaRemainingPercent":81,"dailyQuotaResetAtUnix":"` +
		strconv.FormatInt(dailyReset, 10) + `","weeklyQuotaResetAtUnix":"` + strconv.FormatInt(weeklyReset, 10) + `"}}}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(statusJSON))
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	creds := "windsurf_api_key = \"devin-synthetic\"\napi_server_url = \"" + srv.URL + "\"\n"
	if err := os.WriteFile(devin.CredentialsPath(dataDir), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := devin.SessionDBPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), dbPath, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT)`,
		`CREATE TABLE message_nodes (session_id TEXT, chat_message TEXT, created_at INTEGER)`,
		`INSERT INTO sessions(id, working_directory) VALUES ('s1', '/tmp/devin-proj')`,
	} {
		if _, err := db.Execute(context.Background(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	msg1 := `{"role":"assistant","metadata":{"generation_model":"claude-opus-5-5-medium","created_at":"` + quotaOnlyNow.Add(-time.Hour).Format(time.RFC3339) + `","metrics":{"input_tokens":100000,"output_tokens":10000}}}`
	msg2 := `{"role":"assistant","metadata":{"generation_model":"claude-opus-5-5-medium","created_at":"` + quotaOnlyNow.Add(-36*time.Hour).Format(time.RFC3339) + `","metrics":{"input_tokens":100000,"output_tokens":10000}}}`
	for _, m := range []struct {
		body string
		ts   int64
	}{{msg1, quotaOnlyNow.Add(-time.Hour).Unix()}, {msg2, quotaOnlyNow.Add(-36 * time.Hour).Unix()}} {
		if _, err := db.Execute(context.Background(), `INSERT INTO message_nodes(session_id, chat_message, created_at) VALUES ('s1', ?, ?)`, m.body, m.ts); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	snap := testQuotaOnlyPricing(t, "1")
	src := &devinSource{dir: dataDir, client: devin.Client{HTTP: srv.Client(), Version: "test"}}
	b := newQuotaOnlyBridge(store.ProviderDevin, src, quotaOnlyPrefs(), []string{"en"})
	b.dataDir = dataDir
	b.now = func() time.Time { return quotaOnlyNow }
	b.pricingSnapshot = func() pricing.Snapshot { return snap }

	var scans atomic.Int32
	b.eventsScanner = func(ctx context.Context, w usage.Window, now time.Time) ([]usage.PricedEvent, string, error) {
		scans.Add(1)
		return (devin.SessionScanner{DataDir: dataDir, Location: time.Local, PricingSnapshot: b.pricingSnapshot}).ScanEvents(ctx, w, now)
	}

	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if scans.Load() != 1 {
		t.Fatalf("scans = %d, want 1", scans.Load())
	}
	st := b.State(quotaOnlyNow)
	if len(st.WindowCosts) != 2 {
		t.Fatalf("windowCosts = %+v, want 2", st.WindowCosts)
	}
	if st.WindowCosts[0].WindowMinutes != runway.DailyMinutes || st.WindowCosts[0].USD.String() != "0.75" {
		t.Fatalf("daily windowCost = %+v", st.WindowCosts[0])
	}
	if st.WindowCosts[1].WindowMinutes != runway.WeeklyMinutes || st.WindowCosts[1].USD.String() != "1.5" {
		t.Fatalf("weekly windowCost = %+v", st.WindowCosts[1])
	}

	// Second Refresh with unchanged DB reuses cached WindowCosts without rescanning.
	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if scans.Load() != 1 {
		t.Fatalf("unchanged DB triggered rescan: scans = %d", scans.Load())
	}

	// Detail query works for Devin & Droid and no longer returns errDetailUnavailable.
	rt := &liveApp{
		devinB: b,
		droidB: func() *quotaOnlyBridge {
			db := newDroidBridge(t.TempDir(), quotaOnlyPrefs(), []string{"en"})
			db.pricingSnapshot = func() pricing.Snapshot { return snap }
			return db
		}(),
	}
	var actions PanelActions
	installDetailActions(rt, &actions)
	rt.addForkPanelActions(&actions, nil)
	for _, r := range store.CostRanges() {
		sum, err := actions.QueryCost(context.Background(), store.ProviderDevin, CostQuery{Range: r})
		if err != nil {
			t.Fatalf("devin range %s: %v", r, err)
		}
		if r == store.CostRangeCurrent && (sum.EstimatedUSD == nil || sum.EstimatedUSD.String() != "1.5") {
			t.Fatalf("devin detail current = %+v", sum)
		}
	}
	custom, ok := usage.CustomRange(quotaOnlyNow.Add(-2*time.Hour), quotaOnlyNow, time.Local)
	if !ok {
		t.Fatal("invalid custom range")
	}
	if sum, err := actions.QueryCost(context.Background(), store.ProviderDevin, CostQuery{Custom: &custom}); err != nil || sum.Totals.Turns != 1 {
		t.Fatalf("devin custom = %+v, %v", sum, err)
	}
	if _, err := actions.QueryCost(context.Background(), store.ProviderDroid, CostQuery{Range: store.CostRangeToday}); err != nil {
		t.Fatalf("droid detail = %v", err)
	}
	if _, err := actions.QueryCost(context.Background(), store.ProviderCopilot, CostQuery{Range: store.CostRangeToday}); !errors.Is(err, errDetailUnavailable) {
		t.Fatalf("copilot should stay unavailable, got %v", err)
	}
}

func TestForkPanelActionsWrapQueryCost(t *testing.T) {
	snap := testQuotaOnlyPricing(t, "1")
	db := newDroidBridge(t.TempDir(), quotaOnlyPrefs(), []string{"en"})
	db.pricingSnapshot = func() pricing.Snapshot { return snap }
	rt := &liveApp{droidB: db}

	var baseCalls []store.Provider
	actions := PanelActions{
		QueryCost: func(_ context.Context, p store.Provider, _ CostQuery) (usage.Summary, error) {
			baseCalls = append(baseCalls, p)
			return usage.Summary{Source: usage.SourceLocalSessions}, nil
		},
	}
	rt.addForkPanelActions(&actions, nil)

	if _, err := actions.QueryCost(context.Background(), store.ProviderDroid, CostQuery{Range: store.CostRangeToday}); err != nil {
		t.Fatalf("droid QueryCost = %v", err)
	}
	if _, err := actions.QueryCost(context.Background(), store.ProviderCopilot, CostQuery{Range: store.CostRangeToday}); !errors.Is(err, errDetailUnavailable) {
		t.Fatalf("copilot QueryCost = %v, want errDetailUnavailable", err)
	}
	sum, err := actions.QueryCost(context.Background(), store.ProviderCodex, CostQuery{Range: store.CostRangeToday})
	if err != nil || sum.Source != usage.SourceLocalSessions || len(baseCalls) != 1 || baseCalls[0] != store.ProviderCodex {
		t.Fatalf("codex QueryCost = (%+v, %v), baseCalls=%v", sum, err, baseCalls)
	}
}

func TestPanelQuotaOnlyRendersAPICostSection(t *testing.T) {
	l := l10n.New(l10n.English)
	snap := quotaOnlyFixture()
	snap.Droid.Meters[0].ResetsAt = time.Now().Add(2 * time.Hour)
	snap.Droid.Meters[0].UsedPercent = 25
	snap.Droid.Meters[1].ResetsAt = time.Now().Add(24 * time.Hour)
	snap.Droid.WindowCosts = []runway.QuotaOnlyWindowCost{
		{Source: runway.SourceStandard, WindowMinutes: runway.FiveHourMinutes, USD: decimal.MustParse("12.50"), Priced: true},
		{Source: runway.SourceModelSpecific, WindowMinutes: runway.WeeklyMinutes, USD: decimal.MustParse("99.00"), Priced: true},
	}
	a, tt := newPanel(t, func(p *store.Preferences) {
		enableQuotaOnly(p)
		p.SelectedProvider = store.ProviderDroid
		p.ShowsSpeedAnalysis = false
		p.ShowsCacheAnalysis = false
		p.ShowsCostSummary = true
	}, snap, PanelActions{})
	if !tt.HasText(l.Text(l10n.KeyApiCost)) {
		t.Fatalf("standalone API cost section should be shown for Droid: %q", tt.Texts())
	}
	wantLine := "$12.50 / $50.00 · " + l.Text(l10n.KeyCursorEstimated)
	if !tt.HasText(wantLine) || !tt.HasText(l.Text(l10n.KeyShowDetails)) {
		t.Fatalf("window cost line or Show details missing for Droid: %q", tt.Texts())
	}
	if tt.HasText("$99.00") {
		t.Fatalf("Core window cost should not be shown: %q", tt.Texts())
	}
	if err := tt.Click(l.Text(l10n.KeyShowDetails)); err != nil {
		t.Fatal(err)
	}
	if a.panelState.detail != store.ModuleAPICost {
		t.Fatalf("detail = %q, want %q", a.panelState.detail, store.ModuleAPICost)
	}

	// Hidden when ShowsCostSummary is false.
	_, hidden := newPanel(t, func(p *store.Preferences) {
		enableQuotaOnly(p)
		p.SelectedProvider = store.ProviderDroid
		p.ShowsSpeedAnalysis = false
		p.ShowsCacheAnalysis = false
		p.ShowsCostSummary = false
	}, snap, PanelActions{})
	if hidden.HasText(wantLine) || hidden.HasText(l.Text(l10n.KeyApiCost)) || hidden.HasText(l.Text(l10n.KeyShowDetails)) {
		t.Fatalf("cost shown while disabled: %q", hidden.Texts())
	}
}

func TestQuotaOnlyBridgeCostSummaryRangeAndToggle(t *testing.T) {
	pinQuotaOnlyLocalUTC(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".factory")
	writeDroidLogin(t, dir, "ada@example.com", quotaOnlyNow.Add(time.Hour))
	sessionsDir := filepath.Join(dir, "sessions")
	projDir := filepath.Join(sessionsDir, "my-app")
	if err := os.MkdirAll(projDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSess := func(name string, age time.Duration) {
		p := filepath.Join(projDir, name+".settings.json")
		if err := os.WriteFile(p, []byte(`{"model":"kimi-k3","tokenUsage":{"inputTokens":1000000,"outputTokens":100000}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := quotaOnlyNow.Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	writeSess("today", time.Hour)
	writeSess("this-week", 24*time.Hour)

	limitsBody := `{"hasActiveSubscription":true,"limits":{"standard":{"weekly":{"usedPercent":40,"windowEnd":"2026-11-26T00:00:00Z"}}}}`
	b := droidTestBridge(t, dir, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(limitsBody))
	})
	b.sessionsDir = sessionsDir
	snap := testQuotaOnlyPricing(t, "1")
	b.pricingSnapshot = func() pricing.Snapshot { return snap }

	p := quotaOnlyPrefs()
	p.ShowsCostSummary = true
	p.ShowsSpeedAnalysis = false
	p.APICostSummaryRange = store.CostRangeCurrent
	b.setPreferences(p, []string{"en"})

	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st := b.State(quotaOnlyNow)
	if st.Cost == nil || st.Cost.EstimatedUSD == nil || st.Cost.EstimatedUSD.String() != "2.8" {
		t.Fatalf("current cost = %+v, want $2.80", st.Cost)
	}

	// Switching APICostSummaryRange to Today invalidates old Cost and recomputes on RefreshLocal.
	pToday := p.Clone()
	pToday.APICostSummaryRange = store.CostRangeToday
	b.setPreferences(pToday, []string{"en"})
	if got := b.State(quotaOnlyNow).Cost; got != nil {
		t.Fatalf("changing costRange did not clear stale Cost: %+v", got)
	}
	if err := b.RefreshLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	stToday := b.State(quotaOnlyNow)
	if stToday.Cost == nil || stToday.Cost.EstimatedUSD == nil || stToday.Cost.EstimatedUSD.String() != "1.4" {
		t.Fatalf("today cost = %+v, want $1.40", stToday.Cost)
	}

	// Re-rendering on language switch keeps Cost.
	zh := pToday.Clone()
	zh.Language = store.LanguageSimplifiedChinese
	b.setPreferences(zh, nil)
	if got := b.State(quotaOnlyNow).Cost; got == nil || got.EstimatedUSD == nil || got.EstimatedUSD.String() != "1.4" {
		t.Fatalf("language switch lost Cost: %+v", got)
	}

	// Disabling ShowsCostSummary clears Cost.
	off := zh.Clone()
	off.ShowsCostSummary = false
	b.setPreferences(off, nil)
	if got := b.State(quotaOnlyNow).Cost; got != nil {
		t.Fatalf("disabling ShowsCostSummary did not clear Cost: %+v", got)
	}
}

func TestForkPanelActionsWrapQueryCache(t *testing.T) {
	droidDir := t.TempDir()
	devinDir := t.TempDir()
	db := newDroidBridge(droidDir, quotaOnlyPrefs(), []string{"en"})
	vb := newDevinBridge(devinDir, "test", quotaOnlyPrefs(), []string{"en"})
	db.cacheScanner = func(_ context.Context, _ usage.Window, _ time.Time) (cachehit.Report, error) {
		return cachehit.Report{Totals: cachehit.Totals{InputTokens: 100, CachedInputTokens: 90, Samples: 1}}, nil
	}
	vb.cacheScanner = func(_ context.Context, _ usage.Window, _ time.Time) (cachehit.Report, error) {
		return cachehit.Report{Totals: cachehit.Totals{InputTokens: 200, CachedInputTokens: 150, Samples: 1}}, nil
	}
	rt := &liveApp{droidB: db, devinB: vb}

	var baseCalls []store.Provider
	actions := PanelActions{
		QueryCache: func(_ context.Context, p store.Provider, _ CostQuery) (cachehit.Report, error) {
			baseCalls = append(baseCalls, p)
			return cachehit.Report{Totals: cachehit.Totals{InputTokens: 50, CachedInputTokens: 25, Samples: 1}}, nil
		},
	}
	rt.addForkPanelActions(&actions, nil)

	dr, err := actions.QueryCache(context.Background(), store.ProviderDroid, CostQuery{Range: store.CostRangeToday})
	if err != nil || dr.Totals.CachedInputTokens != 90 {
		t.Fatalf("droid QueryCache = (%+v, %v)", dr, err)
	}
	vr, err := actions.QueryCache(context.Background(), store.ProviderDevin, CostQuery{Range: store.CostRangeToday})
	if err != nil || vr.Totals.CachedInputTokens != 150 {
		t.Fatalf("devin QueryCache = (%+v, %v)", vr, err)
	}
	if _, err := actions.QueryCache(context.Background(), store.ProviderCopilot, CostQuery{Range: store.CostRangeToday}); !errors.Is(err, errInvalidCacheProvider) {
		t.Fatalf("copilot QueryCache = %v, want errInvalidCacheProvider", err)
	}
	cr, err := actions.QueryCache(context.Background(), store.ProviderCodex, CostQuery{Range: store.CostRangeToday})
	if err != nil || cr.Totals.CachedInputTokens != 25 || len(baseCalls) != 1 || baseCalls[0] != store.ProviderCodex {
		t.Fatalf("codex QueryCache = (%+v, %v), baseCalls=%v", cr, err, baseCalls)
	}
}

func TestQuotaOnlyBridgeTodayCostFallback(t *testing.T) {
	pinQuotaOnlyLocalUTC(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".factory")
	writeDroidLogin(t, dir, "ada@example.com", quotaOnlyNow.Add(time.Hour))
	sessionsDir := filepath.Join(dir, "sessions")
	projDir := filepath.Join(sessionsDir, "my-app")
	if err := os.MkdirAll(projDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSess := func(name string, age time.Duration) {
		p := filepath.Join(projDir, name+".settings.json")
		if err := os.WriteFile(p, []byte(`{"model":"kimi-k3","tokenUsage":{"inputTokens":1000000,"outputTokens":100000}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := quotaOnlyNow.Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// Only a session from yesterday (inside Current weekly window, outside Today).
	writeSess("yesterday", 24*time.Hour)

	limitsBody := `{"hasActiveSubscription":true,"limits":{"standard":{"weekly":{"usedPercent":40,"windowEnd":"2026-11-26T00:00:00Z"}}}}`
	b := droidTestBridge(t, dir, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(limitsBody))
	})
	b.sessionsDir = sessionsDir
	snap := testQuotaOnlyPricing(t, "1")
	b.pricingSnapshot = func() pricing.Snapshot { return snap }

	p := quotaOnlyPrefs()
	p.ShowsCostSummary = true
	p.ShowsSpeedAnalysis = false
	p.APICostSummaryRange = store.CostRangeToday
	b.setPreferences(p, []string{"en"})

	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st := b.State(quotaOnlyNow)
	if st.CostRange != store.CostRangeCurrent || st.Cost == nil || !st.Cost.IsDisplayableCost() || st.Cost.EstimatedUSD.String() != "1.4" {
		t.Fatalf("today empty fallback state = (range=%s, cost=%+v)", st.CostRange, st.Cost)
	}

	// forkProviderView uses CostRangeCurrent for CostSubtitle.
	l := l10n.New(l10n.English)
	viewSnap := quotaOnlyFixture()
	viewSnap.Provider = store.ProviderDroid
	viewSnap.Droid = st
	view, ok := forkProviderView(l, viewSnap, store.CostRangeToday)
	wantSub := runway.CostSubtitle(l, *st.Cost, store.CostRangeCurrent, time.Now())
	if !ok || view.DisplayedRange != store.CostRangeCurrent || view.CostSubtitle != wantSub {
		t.Fatalf("forkProviderView = (range=%s, sub=%q), want (current, %q)", view.DisplayedRange, view.CostSubtitle, wantSub)
	}

	// When ThisMonth is selected and has no fallback, CostRange stays ThisMonth (tested on empty earlier range? Use today session check next).
	pMonth := p.Clone()
	pMonth.APICostSummaryRange = store.CostRangeThisMonth
	b.setPreferences(pMonth, []string{"en"})
	if err := b.RefreshLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stMonth := b.State(quotaOnlyNow); stMonth.CostRange != store.CostRangeThisMonth {
		t.Fatalf("ThisMonth CostRange = %s, want thisMonth", stMonth.CostRange)
	}

	// Add a session today: Today does NOT fall back to Current.
	writeSess("today", time.Hour)
	b.setPreferences(p, []string{"en"})
	if err := b.RefreshLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	stToday := b.State(quotaOnlyNow)
	if stToday.CostRange != store.CostRangeToday || stToday.Cost == nil || stToday.Cost.EstimatedUSD.String() != "1.4" {
		t.Fatalf("today with data = (range=%s, cost=%+v)", stToday.CostRange, stToday.Cost)
	}
}

func TestDroidMetersIgnoresCorePool(t *testing.T) {
	lc := l10n.New(l10n.English)
	limits := droid.Limits{
		Standard:  droid.Pool{FiveHour: &droid.Window{UsedPercent: 10, EndsAt: quotaOnlyNow.Add(time.Hour)}},
		Core:      &droid.Pool{Weekly: &droid.Window{UsedPercent: 75, EndsAt: quotaOnlyNow.Add(24 * time.Hour)}},
		UpdatedAt: quotaOnlyNow,
	}
	meters := droidMeters(lc, limits)
	if len(meters) != 1 || meters[0].Source != runway.SourceStandard || meters[0].Title != "Standard · 5-hour" {
		t.Fatalf("droidMeters = %+v, want only Standard meter", meters)
	}
}

func TestQuotaOnlyBridgeSpeedScanToggleAndRefreshSection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".factory")
	writeDroidLogin(t, dir, "ada@example.com", quotaOnlyNow.Add(time.Hour))
	b := droidTestBridge(t, dir, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(droidLimitsBody))
	})
	p := quotaOnlyPrefs()
	p.ShowsCostSummary = false
	p.ShowsSpeedAnalysis = true
	b.setPreferences(p, []string{"en"})

	sampleSessions := []speed.Session{{
		ID: "s1", Title: "Droid session", ProjectName: "proj", UpdatedAt: quotaOnlyNow.Add(-time.Minute),
		Samples: []speed.Sample{{At: quotaOnlyNow.Add(-time.Minute), Model: "claude-opus-5-5", OutputTokens: 200, Decode: 4 * time.Second}},
	}}
	var scanErr error
	var scans atomic.Int32
	b.speedScanner = func(context.Context, time.Time) ([]speed.Session, error) {
		scans.Add(1)
		if scanErr != nil {
			return nil, scanErr
		}
		return sampleSessions, nil
	}

	// Refresh scans speed and sets NoTTFT for Droid.
	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st := b.State(quotaOnlyNow)
	if st.Speed == nil || !st.Speed.NoTTFT || len(st.Speed.Groups) != 1 || st.Speed.Groups[0].OutputTokens != 200 {
		t.Fatalf("speed state = %+v", st.Speed)
	}

	// Re-rendering on language switch preserves Speed.
	zh := p.Clone()
	zh.Language = store.LanguageSimplifiedChinese
	b.setPreferences(zh, nil)
	if got := b.State(quotaOnlyNow); got.Speed == nil || len(got.Speed.Groups) != 1 {
		t.Fatalf("setPreferences lost Speed: %+v", got.Speed)
	}

	// Subsequent Refresh re-renders state without losing Speed even before rescan.
	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if b.State(quotaOnlyNow).Speed == nil {
		t.Fatal("Refresh lost Speed")
	}

	// Scan error preserves previous Speed and sets SpeedError.
	scanErr = errors.New("speed read boom")
	if err := b.RefreshLocal(context.Background()); err == nil {
		t.Fatal("expected RefreshLocal speed error")
	}
	errSt := b.State(quotaOnlyNow)
	if errSt.Speed == nil || errSt.SpeedError == "" {
		t.Fatalf("error state = %+v", errSt)
	}

	// Disabling ShowsSpeedAnalysis clears Speed and SpeedError without scanning.
	off := zh.Clone()
	off.ShowsSpeedAnalysis = false
	b.setPreferences(off, nil)
	cleared := b.State(quotaOnlyNow)
	if cleared.Speed != nil || cleared.SpeedError != "" || cleared.Availability != runway.QuotaOnlyReady {
		t.Fatalf("cleared state = %+v", cleared)
	}
	before := scans.Load()
	if err := b.RefreshLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scans.Load() != before {
		t.Fatalf("hidden speed was scanned: %d -> %d", before, scans.Load())
	}

	// RefreshSection(SectionSpeed) routes to RefreshQuotaOnlyLocal via runway.Service.
	scanErr = nil
	b.setPreferences(p, []string{"en"})
	svc := runway.New(runway.Options{
		Preferences: func() store.Preferences {
			pr := p.Clone()
			pr.SelectedProvider = store.ProviderDroid
			return pr
		}(),
		Droid: b,
	})
	defer svc.Close()
	before = scans.Load()
	refreshSection(svc, runway.SectionSpeed)
	waitSpeedCond(t, "droid speed section refresh", func() bool { return scans.Load() > before })
}

// pinQuotaOnlyLocalUTC makes the local calendar day of the cost windows match
// the UTC fixture times on every runner.
func pinQuotaOnlyLocalUTC(t *testing.T) {
	t.Helper()
	previous := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previous })
}
