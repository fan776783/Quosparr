package copilot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const creditResponse = `{"token_based_billing":true,"copilot_plan":"business","quota_reset_date":"2026-12-01","quota_snapshots":{"premium_interactions":{"entitlement":40000,"remaining":35801,"quota_remaining":35801.5,"percent_remaining":89.5,"credits_used":4198,"overage_count":0,"overage_permitted":true}}}`

func TestQuotaCreditsPrecisionAndBilling(t *testing.T) {
	q, e := ParseQuota([]byte(creditResponse), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if *q.Remaining != 35801.5 || *q.Used != 4198.5 || *q.Limit != 40000 || *q.PercentRemaining != 89.5 || q.ResetsAt.Location() != time.UTC {
		t.Fatal("credit units, precision or reset changed")
	}
	for _, body := range []string{strings.Replace(creditResponse, `"token_based_billing":true`, `"token_based_billing":false`, 1), strings.Replace(creditResponse, `"token_based_billing":true,`, "", 1)} {
		if _, e := ParseQuota([]byte(body), time.Now()); !errors.Is(e, ErrBilling) {
			t.Fatal("legacy or unknown billing was shown as AI Credits")
		}
	}
	for _, body := range []string{strings.Replace(creditResponse, "89.5", "100.1", 1), strings.Replace(creditResponse, "35801.5", "-1", 1), strings.Replace(creditResponse, "35801.5", "40001", 1), `{"token_based_billing":true}`, `null`} {
		if _, e := ParseQuota([]byte(body), time.Now()); e == nil {
			t.Fatal("invalid response was accepted")
		}
	}
}
func TestQuotaSharedAndFree(t *testing.T) {
	q, e := ParseQuota([]byte(`{"token_based_billing":true,"quota_snapshots":{"premium_interactions":{"unlimited":true,"entitlement":-1,"percent_remaining":100,"credits_used":12.75,"has_quota":true}}}`), time.Now())
	if e != nil || q.Limit != nil || q.Remaining != nil || q.PercentRemaining != nil || q.Used == nil || *q.Used != 12.75 {
		t.Fatal("unlimited/shared allowance acquired a denominator")
	}
	q, e = ParseQuota([]byte(`{"token_based_billing":true,"copilot_plan":"free","quota_snapshots":{"premium_interactions":{"entitlement":0,"percent_remaining":0},"chat":{"entitlement":100,"quota_remaining":25.5,"percent_remaining":25.5,"has_quota":false}}}`), time.Now())
	if e != nil || q.Remaining == nil || *q.Remaining != 25.5 || q.Used == nil || *q.Used != 74.5 {
		t.Fatal("Free AI Credits did not use its allocated category")
	}
}
func TestClientReadOnlyIdentityAndFailures(t *testing.T) {
	status := 200
	login := "chosen"
	redirectCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalls++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "token synthetic-credential" {
			t.Error("unexpected quota request")
		}
		if r.URL.Path == "/user" {
			fmt.Fprintf(w, `{"login":%q,"id":7}`, login)
			return
		}
		if r.Header.Get("X-GitHub-Api-Version") != "2025-04-01" {
			t.Error("API version missing")
		}
		if status == 302 {
			http.Redirect(w, r, target.URL, 302)
			return
		}
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(status)
		fmt.Fprint(w, creditResponse)
	}))
	defer server.Close()
	c := Client{HTTP: server.Client(), endpoint: server.URL + "/copilot_internal/user"}
	a := Auth{ID: "7", Login: "chosen", Host: "https://github.com", Token: "synthetic-credential"}
	if _, e := c.FetchAccount(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	login = "other"
	if _, e := c.FetchAccount(context.Background(), a); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("wrong account quota accepted")
	}
	for _, v := range []int{401, 403, 429, 302, 500} {
		status = v
		_, e := c.Fetch(context.Background(), a.Token)
		if e == nil {
			t.Fatal("HTTP failure appeared as zero quota")
		}
		if v == 429 {
			var limited *RateLimitedError
			if !errors.As(e, &limited) || limited.RetryAfter != 12*time.Second {
				t.Fatal("Retry-After lost")
			}
		}
	}
	if redirectCalls != 0 {
		t.Fatal("credentials followed a redirect")
	}
}

func TestQuotaResetPrefersCategoryThenUTCDate(t *testing.T) {
	category := time.Date(2026, 12, 2, 4, 5, 6, 0, time.UTC)
	account := time.Date(2026, 12, 1, 18, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, resetFields, categoryFields string
		want                              time.Time
	}{
		{"category-overrides-account", `"quota_reset_date":"not-a-date","quota_reset_date_utc":"2026-12-01T18:30:00Z"`, fmt.Sprintf(`"quota_reset_at":%d,`, category.Unix()), category},
		{"utc-overrides-date", `"quota_reset_date":"2026-12-01","quota_reset_date_utc":"2026-12-01T18:30:00Z"`, "", account},
		{"only-utc", `"quota_reset_date_utc":"2026-12-01T18:30:00Z"`, "", account},
		{"zero-category-falls-back", `"quota_reset_date":"2026-12-01","quota_reset_date_utc":"2026-12-01T18:30:00Z"`, `"quota_reset_at":0,`, account},
		{"date-fallback", `"quota_reset_date":"2026-12-01"`, "", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(creditResponse, `"quota_reset_date":"2026-12-01"`, tc.resetFields, 1)
			body = strings.Replace(body, `"entitlement":40000`, tc.categoryFields+`"entitlement":40000`, 1)
			q, e := ParseQuota([]byte(body), time.Now())
			if e != nil || !q.ResetsAt.Equal(tc.want) {
				t.Fatalf("reset precedence failed: got %s, error %v", q.ResetsAt, e)
			}
		})
	}
}
