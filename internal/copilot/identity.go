package copilot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/licoy/quosparr/internal/netx"
)

// FetchAccount verifies the selected identity before publishing its quota.
// A credential from another login must never be labelled as the chosen account.
func (c Client) FetchAccount(ctx context.Context, a Auth) (Snapshot, error) {
	if a.Host != "https://github.com" || !validToken(a.Token) {
		return Snapshot{}, ErrCredentials
	}
	q, err := c.Fetch(ctx, a.Token)
	if err != nil {
		return Snapshot{}, err
	}
	h := c.HTTP
	if h == nil {
		h, err = netx.ClientFor(ctx, netx.WithoutCookies)
		if err != nil {
			return Snapshot{}, ErrUnavailable
		}
	}
	client := *h
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	url := "https://api.github.com/user"
	if c.endpoint != "" {
		url = strings.TrimSuffix(c.endpoint, "/copilot_internal/user") + "/user"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	req.Header.Set("Authorization", "token "+a.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, ErrUnauthorized
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Snapshot{}, ErrResponse
	}
	var user struct {
		Login string          `json:"login"`
		ID    json.RawMessage `json:"id"`
	}
	if json.Unmarshal(data, &user) != nil {
		return Snapshot{}, ErrResponse
	}
	if !strings.EqualFold(user.Login, a.Login) || a.ID != a.Login && strings.Trim(string(user.ID), "\"") != a.ID {
		return Snapshot{}, ErrUnauthorized
	}
	return q, nil
}
