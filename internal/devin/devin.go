package devin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/licoy/quosparr/internal/netx"
)

const (
	// DefaultOrigin is the API origin when credentials.toml names none.
	DefaultOrigin = "https://server.codeium.com"
	// OriginEnv overrides the origin for local testing.
	OriginEnv = "QUOSPARR_DEV_DEVIN_API_URL"

	statusPath     = "/exa.seat_management_pb.SeatManagementService/GetUserStatus"
	quotaStrategy  = "BILLING_STRATEGY_QUOTA"
	fileLimit      = 64 << 10
	responseLimit  = 1 << 20
	requestTimeout = 20 * time.Second
)

// Errors of Load and Fetch.
var (
	ErrNotInstalled     = errors.New("devin: not installed")
	ErrNotLoggedIn      = errors.New("devin: not signed in")
	ErrUnsupportedLogin = errors.New("devin: login has no subscription quota")
	ErrLoginUnreadable  = errors.New("devin: credentials unreadable")
	ErrUnauthorized     = errors.New("devin: login rejected")
	ErrRateLimited      = errors.New("devin: rate limited")
	ErrBadResponse      = errors.New("devin: unreadable status response")
	ErrRequestFailed    = errors.New("devin: request failed")
)

// Login is the CLI login. String never shows the key.
type Login struct {
	APIKey string
	Origin string
}

func (l Login) String() string { return "devin.Login{" + l.Origin + "}" }

// DataDir is the Devin CLI data directory for home and the environment.
func DataDir(home string, getenv func(string) string) string {
	if runtime.GOOS == "windows" {
		if v := getenv("APPDATA"); v != "" {
			return filepath.Join(v, "devin")
		}
		return filepath.Join(home, "AppData", "Roaming", "devin")
	}
	if v := getenv("XDG_DATA_HOME"); v != "" && filepath.IsAbs(v) {
		return filepath.Join(v, "devin")
	}
	return filepath.Join(home, ".local", "share", "devin")
}

// CredentialsPath is the login file of dir.
func CredentialsPath(dir string) string { return filepath.Join(dir, "credentials.toml") }

// Load reads credentials.toml of dir.
func Load(dir string) (Login, error) {
	f, err := os.Open(CredentialsPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		if info, serr := os.Stat(dir); serr != nil || !info.IsDir() {
			return Login{}, ErrNotInstalled
		}
		return Login{}, ErrNotLoggedIn
	}
	if err != nil {
		return Login{}, ErrLoginUnreadable
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, fileLimit+1))
	if err != nil || len(data) > fileLimit {
		return Login{}, ErrLoginUnreadable
	}
	var doc struct {
		WindsurfAPIKey string `toml:"windsurf_api_key"`
		APIServerURL   string `toml:"api_server_url"`
	}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return Login{}, ErrLoginUnreadable
	}
	key := strings.TrimSpace(doc.WindsurfAPIKey)
	if key == "" {
		return Login{}, ErrUnsupportedLogin
	}
	origin := DefaultOrigin
	if v := strings.TrimSpace(doc.APIServerURL); v != "" {
		u, err := url.Parse(v)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return Login{}, ErrLoginUnreadable
		}
		origin = strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
	}
	return Login{APIKey: key, Origin: origin}, nil
}

// Status is one GetUserStatus reading. The windows are nil when the plan
// reports none (or hides the daily one).
type Status struct {
	Name, Email string
	Plan        string
	Daily       *Window
	Weekly      *Window
	UpdatedAt   time.Time
}

// Window is a remaining percentage and its reset, zero when unknown.
type Window struct {
	RemainingPercent int
	ResetsAt         time.Time
}

// Client reads the status. The zero value uses package netx.
type Client struct {
	HTTP *http.Client
	// Version is the Quosparr version sent in the request metadata.
	Version string
}

// Fetch reads the user status of login.
func (c Client) Fetch(ctx context.Context, login Login, now time.Time) (Status, error) {
	origin := login.Origin
	if v := strings.TrimSpace(os.Getenv(OriginEnv)); v != "" {
		origin = strings.TrimRight(v, "/")
	}
	version := c.Version
	if version == "" {
		version = "dev"
	}
	body, _ := json.Marshal(map[string]any{"metadata": map[string]string{
		"apiKey": login.APIKey, "ideName": "quosparr", "ideVersion": version, "extensionVersion": version, "locale": "en",
	}})
	data, err := c.post(ctx, origin+statusPath, body)
	if err != nil {
		return Status{}, err
	}
	st, err := ParseStatus(data)
	if err != nil {
		return Status{}, err
	}
	st.UpdatedAt = now
	return st, nil
}

func (c Client) post(ctx context.Context, rawURL string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	client := c.HTTP
	if client == nil {
		var err error
		if client, err = netx.ClientFor(ctx, netx.WithoutCookies); err != nil {
			return nil, ErrRequestFailed
		}
	}
	cp := *client
	cp.Jar = nil
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, ErrRequestFailed
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	resp, err := cp.Do(req)
	if err != nil {
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ctx.Err()
		}
		return nil, ErrRequestFailed
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return nil, ErrRequestFailed
	}
	if len(data) > responseLimit {
		return nil, ErrBadResponse
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return data, nil
	}
	var connect struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(data, &connect)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
		connect.Code == "unauthenticated", connect.Code == "permission_denied":
		return nil, ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests, connect.Code == "resource_exhausted":
		return nil, ErrRateLimited
	}
	return nil, fmt.Errorf("%w: HTTP %d", ErrRequestFailed, resp.StatusCode)
}

// unixField is an int64 that proto3 JSON writes as a string or a number.
type unixField int64

func (u *unixField) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*u = unixField(v)
	return nil
}

// ParseStatus decodes a GetUserStatus JSON response.
func ParseStatus(data []byte) (Status, error) {
	var doc struct {
		UserStatus *struct {
			Name       string `json:"name"`
			Email      string `json:"email"`
			PlanStatus *struct {
				PlanInfo *struct {
					PlanName        string `json:"planName"`
					BillingStrategy string `json:"billingStrategy"`
					HideDailyQuota  bool   `json:"hideDailyQuota"`
				} `json:"planInfo"`
				DailyRemaining  *int      `json:"dailyQuotaRemainingPercent"`
				WeeklyRemaining *int      `json:"weeklyQuotaRemainingPercent"`
				DailyResetAt    unixField `json:"dailyQuotaResetAtUnix"`
				WeeklyResetAt   unixField `json:"weeklyQuotaResetAtUnix"`
			} `json:"planStatus"`
		} `json:"userStatus"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.UserStatus == nil {
		return Status{}, ErrBadResponse
	}
	us := doc.UserStatus
	st := Status{Name: strings.TrimSpace(us.Name), Email: strings.TrimSpace(us.Email)}
	ps := us.PlanStatus
	if ps == nil {
		return st, nil
	}
	if ps.PlanInfo != nil {
		st.Plan = strings.TrimSpace(ps.PlanInfo.PlanName)
	}
	if ps.PlanInfo == nil || ps.PlanInfo.BillingStrategy != quotaStrategy {
		return st, nil
	}
	if !ps.PlanInfo.HideDailyQuota {
		st.Daily = newWindow(ps.DailyRemaining, ps.DailyResetAt)
	}
	st.Weekly = newWindow(ps.WeeklyRemaining, ps.WeeklyResetAt)
	return st, nil
}

func newWindow(remaining *int, reset unixField) *Window {
	w := &Window{}
	if remaining != nil {
		w.RemainingPercent = min(100, max(0, *remaining))
	}
	if reset > 0 {
		w.ResetsAt = time.Unix(int64(reset), 0).UTC()
	}
	return w
}
