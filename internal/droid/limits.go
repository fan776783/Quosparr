package droid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/netx"
)

const (
	// DefaultOrigin is the Factory API origin.
	DefaultOrigin = "https://api.factory.ai"
	// OriginEnv overrides DefaultOrigin for local testing.
	OriginEnv = "QUOSPARR_DEV_DROID_API_URL"

	requestTimeout = 20 * time.Second
	responseLimit  = 1 << 20
)

// Errors of Fetch.
var (
	ErrUnauthorized   = errors.New("droid: login rejected")
	ErrRateLimited    = errors.New("droid: rate limited")
	ErrNoSubscription = errors.New("droid: no active subscription")
	ErrBadResponse    = errors.New("droid: unreadable quota response")
	ErrRequestFailed  = errors.New("droid: request failed")
)

// Window is one quota window. EndsAt is zero while it has not started.
type Window struct {
	UsedPercent float64
	EndsAt      time.Time
}

// Pool is the windows of one model pool; absent windows are nil.
type Pool struct {
	FiveHour, Weekly, Monthly *Window
}

// Limits is one quota reading.
type Limits struct {
	Standard Pool
	// Core is nil when the account has no Core pool.
	Core *Pool
	// ExtraUsageBalanceCents is nil when the response has none.
	ExtraUsageBalanceCents *int64
	// Plan is the raw factoryTier ("team_annual"), "" when unknown.
	Plan      string
	UpdatedAt time.Time
}

// Client reads the quota. The zero value uses the proxy-aware client of
// package netx and the default origin.
type Client struct {
	HTTP   *http.Client
	Origin string
}

func (c Client) origin() string {
	if c.Origin != "" {
		return strings.TrimRight(c.Origin, "/")
	}
	if v := strings.TrimSpace(os.Getenv(OriginEnv)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultOrigin
}

// Fetch reads the billing limits and, best effort, the plan.
func (c Client) Fetch(ctx context.Context, login Login, now time.Time) (Limits, error) {
	data, err := c.get(ctx, "/api/billing/limits", login.AccessToken)
	if err != nil {
		return Limits{}, err
	}
	limits, err := ParseLimits(data)
	if err != nil {
		return Limits{}, err
	}
	limits.UpdatedAt = now
	if me, err := c.get(ctx, "/api/app/auth/me", login.AccessToken); err == nil {
		limits.Plan = parsePlan(me)
	}
	return limits, nil
}

func (c Client) get(ctx context.Context, path, token string) ([]byte, error) {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin()+path, nil)
	if err != nil {
		return nil, ErrRequestFailed
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := cp.Do(req)
	if err != nil {
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ctx.Err()
		}
		return nil, ErrRequestFailed
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, fmt.Errorf("%w: HTTP %d", ErrRequestFailed, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return nil, ErrRequestFailed
	}
	if len(data) > responseLimit {
		return nil, ErrBadResponse
	}
	return data, nil
}

type windowJSON struct {
	UsedPercent *float64 `json:"usedPercent"`
	WindowEnd   *string  `json:"windowEnd"`
}

type poolJSON struct {
	FiveHour *windowJSON `json:"fiveHour"`
	Weekly   *windowJSON `json:"weekly"`
	Monthly  *windowJSON `json:"monthly"`
}

// ParseLimits decodes a /api/billing/limits response.
func ParseLimits(data []byte) (Limits, error) {
	var doc struct {
		HasActiveSubscription  *bool  `json:"hasActiveSubscription"`
		ExtraUsageBalanceCents *int64 `json:"extraUsageBalanceCents"`
		Limits                 *struct {
			Standard *poolJSON `json:"standard"`
			Core     *poolJSON `json:"core"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Limits{}, ErrBadResponse
	}
	if doc.Limits == nil || doc.Limits.Standard == nil {
		if doc.HasActiveSubscription != nil && !*doc.HasActiveSubscription {
			return Limits{}, ErrNoSubscription
		}
		return Limits{}, ErrBadResponse
	}
	std, err := doc.Limits.Standard.pool()
	if err != nil {
		return Limits{}, err
	}
	out := Limits{Standard: std, ExtraUsageBalanceCents: doc.ExtraUsageBalanceCents}
	if doc.Limits.Core != nil {
		core, err := doc.Limits.Core.pool()
		if err != nil {
			return Limits{}, err
		}
		if core.FiveHour != nil || core.Weekly != nil || core.Monthly != nil {
			out.Core = &core
		}
	}
	return out, nil
}

func (p poolJSON) pool() (Pool, error) {
	var out Pool
	for _, w := range []struct {
		in  *windowJSON
		out **Window
	}{{p.FiveHour, &out.FiveHour}, {p.Weekly, &out.Weekly}, {p.Monthly, &out.Monthly}} {
		if w.in == nil || w.in.UsedPercent == nil {
			continue
		}
		win := Window{UsedPercent: *w.in.UsedPercent}
		if w.in.WindowEnd != nil && *w.in.WindowEnd != "" {
			t, err := time.Parse(time.RFC3339Nano, *w.in.WindowEnd)
			if err != nil {
				return Pool{}, ErrBadResponse
			}
			win.EndsAt = t
		}
		*w.out = &win
	}
	if out.FiveHour == nil && out.Weekly == nil && out.Monthly == nil {
		return Pool{}, ErrBadResponse
	}
	return out, nil
}

// parsePlan reads organization.subscription.factoryTier only.
func parsePlan(data []byte) string {
	var doc struct {
		Organization *struct {
			Subscription *struct {
				FactoryTier string `json:"factoryTier"`
			} `json:"subscription"`
		} `json:"organization"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.Organization == nil || doc.Organization.Subscription == nil {
		return ""
	}
	return strings.TrimSpace(doc.Organization.Subscription.FactoryTier)
}

// PlanName is the display name of a factoryTier: the billing period is
// dropped and each word capitalized ("team_annual" is "Team").
func PlanName(tier string) string {
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(tier), func(r rune) bool { return r == '_' || r == '-' || r == ' ' }) {
		switch w {
		case "annual", "annually", "monthly", "yearly", "year", "month":
			continue
		}
		words = append(words, strings.ToUpper(w[:1])+w[1:])
	}
	return strings.Join(words, " ")
}
