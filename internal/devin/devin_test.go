package devin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)

func writeCreds(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "credentials.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoad(t *testing.T) {
	login, err := Load(writeCreds(t, "windsurf_api_key = \"devin-synthetic\"\napi_server_url = \"https://server.example.com/\"\ndevin_api_url = \"https://api.example.com\"\n"))
	if err != nil || login.APIKey != "devin-synthetic" || login.Origin != "https://server.example.com" {
		t.Fatalf("login %v err %v", login, err)
	}
	if strings.Contains(login.String(), login.APIKey) {
		t.Fatal("String shows the key")
	}
	if login, err := Load(writeCreds(t, "windsurf_api_key = \"k\"\n")); err != nil || login.Origin != DefaultOrigin {
		t.Fatalf("default origin: %v %v", login, err)
	}
	cases := map[string]error{
		"devin_api_url = \"https://api.example.com\"\n":                         ErrUnsupportedLogin,
		"windsurf_api_key = \"k\"\napi_server_url = \"http://plain.example\"\n": ErrLoginUnreadable,
		"windsurf_api_key = \n":                                                 ErrLoginUnreadable,
	}
	for body, want := range cases {
		if _, err := Load(writeCreds(t, body)); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", body, err, want)
		}
	}
	if _, err := Load(t.TempDir()); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("empty dir: %v", err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "absent")); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("missing dir: %v", err)
	}
}

func TestDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix layout")
	}
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if got := DataDir("/home/u", get); got != filepath.Join("/home/u", ".local", "share", "devin") {
		t.Fatal(got)
	}
	env["XDG_DATA_HOME"] = "/data"
	if got := DataDir("/home/u", get); got != filepath.Join("/data", "devin") {
		t.Fatal(got)
	}
	env["XDG_DATA_HOME"] = "relative"
	if got := DataDir("/home/u", get); got != filepath.Join("/home/u", ".local", "share", "devin") {
		t.Fatal(got)
	}
}

const statusFixture = `{"userStatus":{"pro":true,"name":"Ada","email":"ada@example.com","teamsTier":"TEAMS_TIER_DEVIN_MAX",
 "planStatus":{"planInfo":{"teamsTier":"TEAMS_TIER_DEVIN_MAX","planName":"Max","billingStrategy":"BILLING_STRATEGY_QUOTA","monthlyPromptCredits":100},
  "availablePromptCredits":100,"dailyQuotaRemainingPercent":100,"weeklyQuotaRemainingPercent":81,
  "dailyQuotaResetAtUnix":"1795248000","weeklyQuotaResetAtUnix":1795334400}},"planInfo":{"planName":"Max"}}`

func TestParseStatus(t *testing.T) {
	st, err := ParseStatus([]byte(statusFixture))
	if err != nil {
		t.Fatal(err)
	}
	if st.Name != "Ada" || st.Email != "ada@example.com" || st.Plan != "Max" {
		t.Fatalf("status %+v", st)
	}
	if st.Daily == nil || st.Daily.RemainingPercent != 100 || st.Daily.ResetsAt.Unix() != 1795248000 {
		t.Fatalf("daily %+v", st.Daily)
	}
	if st.Weekly == nil || st.Weekly.RemainingPercent != 81 || st.Weekly.ResetsAt.Unix() != 1795334400 {
		t.Fatalf("weekly %+v", st.Weekly)
	}
	hidden := strings.Replace(statusFixture, `"monthlyPromptCredits":100`, `"hideDailyQuota":true`, 1)
	hidden = strings.Replace(hidden, `"weeklyQuotaRemainingPercent":81,`, ``, 1)
	st, err = ParseStatus([]byte(hidden))
	if err != nil || st.Daily != nil || st.Weekly == nil || st.Weekly.RemainingPercent != 0 {
		t.Fatalf("hidden daily, exhausted weekly: %+v %v", st, err)
	}
	credits := strings.Replace(statusFixture, "BILLING_STRATEGY_QUOTA", "BILLING_STRATEGY_CREDITS", 1)
	if st, err := ParseStatus([]byte(credits)); err != nil || st.Daily != nil || st.Weekly != nil || st.Plan != "Max" {
		t.Fatalf("credit plan: %+v %v", st, err)
	}
	for _, bad := range []string{`[]`, `{}`, `{"userStatus":{"planStatus":{"dailyQuotaResetAtUnix":"soon"}}}`} {
		if _, err := ParseStatus([]byte(bad)); !errors.Is(err, ErrBadResponse) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != statusPath || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Cookie") != "" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Metadata map[string]string `json:"metadata"`
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		switch body.Metadata["apiKey"] {
		case "good":
			if body.Metadata["ideName"] != "quosparr" || body.Metadata["ideVersion"] != "1.2.3" {
				t.Errorf("metadata %v", body.Metadata)
			}
			_, _ = w.Write([]byte(statusFixture))
		case "denied":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"x"}`))
		case "busy":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"invalid_argument","message":"an internal error occurred"}`))
		}
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), Version: "1.2.3"}
	st, err := c.Fetch(context.Background(), Login{APIKey: "good", Origin: srv.URL}, testNow)
	if err != nil || st.Weekly == nil || !st.UpdatedAt.Equal(testNow) {
		t.Fatalf("status %+v err %v", st, err)
	}
	for key, want := range map[string]error{"denied": ErrUnauthorized, "busy": ErrRateLimited, "other": ErrRequestFailed} {
		if _, err := c.Fetch(context.Background(), Login{APIKey: key, Origin: srv.URL}, testNow); !errors.Is(err, want) {
			t.Errorf("%s: %v", key, err)
		}
	}
}
