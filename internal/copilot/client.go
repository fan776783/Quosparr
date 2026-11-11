// Package copilot monitors GitHub AI Credits without changing editor or CLI
// logins. Credentials are transient, never serialized, and only read after
// opt-in. Legacy billing and unrecognized units are not interpreted as credits.
package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/licoy/quosparr/internal/netx"
)

var (
	ErrLogin        = errors.New("copilot: no readable login")
	ErrCredentials  = errors.New("copilot: credential store unavailable")
	ErrAuthorize    = errors.New("copilot: credential authorization required")
	ErrUnsupported  = errors.New("copilot: unsupported credential encryption")
	ErrUnauthorized = errors.New("copilot: login expired")
	ErrBilling      = errors.New("copilot: AI Credits billing not confirmed")
	ErrResponse     = errors.New("copilot: unsupported quota response")
	ErrUnavailable  = errors.New("copilot: quota unavailable")
	ErrAccount      = errors.New("copilot: select an account")
)

type RateLimitedError struct{ RetryAfter time.Duration }

func (*RateLimitedError) Error() string { return "copilot: rate limited" }

type Snapshot struct {
	Plan                                              string
	Limit, Remaining, Used, PercentRemaining, Overage *float64
	Unlimited, HasQuota, OveragePermitted             bool
	ResetsAt, UpdatedAt                               time.Time
}

func ParseQuota(data []byte, now time.Time) (Snapshot, error) {
	var raw struct {
		TokenBasedBilling *bool  `json:"token_based_billing"`
		Plan              string `json:"copilot_plan"`
		Reset             string `json:"quota_reset_date"`
		ResetUTC          string `json:"quota_reset_date_utc"`
		Snapshots         map[string]struct {
			Entitlement      *float64 `json:"entitlement"`
			Remaining        *float64 `json:"remaining"`
			QuotaRemaining   *float64 `json:"quota_remaining"`
			Percent          *float64 `json:"percent_remaining"`
			Overage          *float64 `json:"overage_count"`
			CreditsUsed      *float64 `json:"credits_used"`
			Unlimited        bool     `json:"unlimited"`
			HasQuota         *bool    `json:"has_quota"`
			OveragePermitted bool     `json:"overage_permitted"`
			ResetAt          *int64   `json:"quota_reset_at"`
		} `json:"quota_snapshots"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return Snapshot{}, ErrResponse
	}
	if raw.TokenBasedBilling == nil || !*raw.TokenBasedBilling {
		return Snapshot{}, ErrBilling
	}
	q, ok := raw.Snapshots["premium_models"]
	if !ok {
		q, ok = raw.Snapshots["premium_interactions"]
	}
	if (!ok || q.Entitlement != nil && *q.Entitlement == 0 && !q.Unlimited) && raw.Plan == "free" {
		q, ok = raw.Snapshots["chat"]
	}
	if !ok || q.Entitlement != nil && *q.Entitlement == 0 && !q.Unlimited {
		return Snapshot{}, ErrResponse
	}
	if q.QuotaRemaining != nil {
		q.Remaining = q.QuotaRemaining
	}
	for _, v := range []*float64{q.Entitlement, q.Remaining, q.Percent, q.Overage, q.CreditsUsed} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 && !(v == q.Entitlement && *v == -1 && q.Unlimited)) {
			return Snapshot{}, ErrResponse
		}
	}
	if q.Percent != nil && *q.Percent > 100 {
		return Snapshot{}, ErrResponse
	}
	if q.Percent == nil && q.Remaining == nil && !q.Unlimited {
		return Snapshot{}, ErrResponse
	}
	out := Snapshot{Plan: raw.Plan, Limit: q.Entitlement, Remaining: q.Remaining, PercentRemaining: q.Percent,
		Overage: q.Overage, Unlimited: q.Unlimited, HasQuota: true, OveragePermitted: q.OveragePermitted, UpdatedAt: now}
	if q.HasQuota != nil {
		out.HasQuota = *q.HasQuota
	}
	if !q.Unlimited && q.Entitlement != nil && q.Remaining != nil {
		if *q.Remaining > *q.Entitlement {
			return Snapshot{}, ErrResponse
		}
		used := *q.Entitlement - *q.Remaining
		out.Used = &used
	}
	if q.Unlimited {
		if out.HasQuota {
			out.Used = q.CreditsUsed
		}
		out.Limit = nil
		out.Remaining = nil
		out.PercentRemaining = nil
	}
	if q.ResetAt != nil && (*q.ResetAt < 0 || *q.ResetAt > 253402300799) {
		return Snapshot{}, ErrResponse
	}
	if q.ResetAt != nil && *q.ResetAt > 0 {
		out.ResetsAt = time.Unix(*q.ResetAt, 0).UTC()
		return out, nil
	}
	// Category-specific timestamps govern that quota. Zero means absent;
	// prefer the account's precise UTC time over its coarser calendar date.
	reset := raw.ResetUTC
	if reset == "" {
		reset = raw.Reset
	}
	if reset != "" {
		var err error
		out.ResetsAt, err = time.Parse(time.RFC3339, reset)
		if err != nil {
			out.ResetsAt, err = time.Parse("2006-01-02", reset)
		}
		if err != nil {
			return Snapshot{}, ErrResponse
		}
		out.ResetsAt = out.ResetsAt.UTC()
	}
	return out, nil
}

type Client struct {
	HTTP     *http.Client
	endpoint string
}

func (c Client) Fetch(ctx context.Context, token string) (Snapshot, error) {
	if !validToken(token) {
		return Snapshot{}, ErrCredentials
	}
	h := c.HTTP
	if h == nil {
		var err error
		h, err = netx.ClientFor(ctx, netx.WithoutCookies)
		if err != nil {
			return Snapshot{}, ErrUnavailable
		}
	}
	client := *h
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	url := c.endpoint
	if url == "" {
		url = "https://api.github.com/copilot_internal/user"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("X-GitHub-Api-Version", "2025-04-01")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Snapshot{}, ctx.Err()
		}
		return Snapshot{}, ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 401, 403:
		return Snapshot{}, ErrUnauthorized
	case 429:
		wait := 5 * time.Minute
		if n, e := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 32); e == nil && n >= 0 {
			wait = time.Duration(n) * time.Second
		} else if at, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
			wait = time.Until(at)
		}
		return Snapshot{}, &RateLimitedError{max(time.Second, wait)}
	default:
		return Snapshot{}, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return Snapshot{}, ErrResponse
	}
	return ParseQuota(data, time.Now())
}
