package copilot

import (
	"context"
	"encoding/json"
	"strings"
)

func (r *Reader) parseCLI(ctx context.Context, data []byte, authorize bool) ([]Auth, error) {
	defer clear(data)
	type user struct{ Host, Login, Kind, Token string }
	var config struct {
		Users []user `json:"loggedInUsers"`
		Last  user   `json:"lastLoggedInUser"`
	}
	if json.Unmarshal(data, &config) != nil {
		return nil, ErrCredentials
	}
	// Observe the CLI's selected account, not another saved login. No fallback
	// to gh/environment tokens: those may belong to an unrelated identity.
	u := config.Last
	if u.Login == "" {
		if len(config.Users) != 1 {
			return nil, ErrLogin
		}
		u = config.Users[0]
	}
	if u.Host != "https://github.com" || u.Login == "" || strings.ContainsAny(u.Login, "\r\n:") {
		return nil, ErrUnsupported
	}
	found := false
	for _, saved := range config.Users {
		if saved.Host == u.Host && saved.Login == u.Login && (saved.Kind == "" || saved.Kind == "github" || saved.Kind == "githubDotCom") {
			found = true
			if validToken(saved.Token) {
				u.Token = saved.Token
			}
			break
		}
	}
	if !found {
		return nil, ErrLogin
	}
	account := u.Host + ":" + u.Login + ":github"
	token := u.Token
	if !authorize && !validToken(token) {
		token = r.tokens[account]
	}
	if !validToken(token) {
		if !authorize {
			return nil, ErrAuthorize
		}
		data, err := r.password(ctx, "copilot-cli", account)
		defer clear(data)
		if err != nil {
			return nil, err
		}
		token = string(data)
		if !validToken(token) {
			return nil, ErrCredentials
		}
		if r.tokens == nil {
			r.tokens = map[string]string{}
		}
		r.tokens[account] = token
	}
	return []Auth{{ID: u.Login, Login: u.Login, Host: u.Host, Token: token}}, nil
}
