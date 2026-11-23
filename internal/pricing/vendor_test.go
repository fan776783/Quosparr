package pricing

import (
	"testing"

	"github.com/licoy/quosparr/internal/usage/decimal"
)

func TestLookupVendor(t *testing.T) {
	d := func(s string) *decimal.Decimal { v := decimal.MustParse(s); return &v }
	snap, err := NewSnapshot([]Model{
		{ProviderID: "anthropic", ID: "claude-opus-5-5", Rate: Rate{Input: d("5"), Output: d("25")}},
		{ProviderID: "anthropic", ID: "claude-fable-5-2", Rate: Rate{Input: d("3"), Output: d("15")}},
		{ProviderID: "openai", ID: "gpt-5.5", Rate: Rate{Input: d("5"), Output: d("30")}},
		{ProviderID: "openai", ID: "o3-mini", Rate: Rate{Input: d("1.1"), Output: d("4.4")}},
		{ProviderID: "openai", ID: "codex-mini", Rate: Rate{Input: d("1.5"), Output: d("6")}},
		{ProviderID: "google", ID: "gemini-4-hydrogen", Rate: Rate{Input: d("1.25"), Output: d("10")}},
		{ProviderID: "xai", ID: "grok-4.6", Rate: Rate{Input: d("2"), Output: d("6")}},
		{ProviderID: "moonshotai", ID: "kimi-k3", Rate: Rate{Input: d("1"), Output: d("4")}},
		{ProviderID: "zhipuai", ID: "glm-5.3", Rate: Rate{Input: d("1"), Output: d("4")}},
		{ProviderID: "deepseek", ID: "deepseek-v4-pro", Rate: Rate{Input: d("0.5"), Output: d("2")}},
		{ProviderID: "alibaba", ID: "qwen-3", Rate: Rate{Input: d("0.4"), Output: d("1.6")}},
		{ProviderID: "minimax", ID: "minimax-m2", Rate: Rate{Input: d("0.3"), Output: d("1.2")}},
		// Third-party aggregator entries must never match via LookupVendor.
		{ProviderID: "openrouter", ID: "swe-2-medium", Rate: Rate{Input: d("9"), Output: d("9")}},
		{ProviderID: "openrouter", ID: "claude-sonnet-9", Rate: Rate{Input: d("9"), Output: d("9")}},
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		model        string
		wantOK       bool
		wantProvider string
		wantID       string
	}{
		{model: "claude-opus-5-5-medium", wantOK: true, wantProvider: "anthropic", wantID: "claude-opus-5-5"},
		{model: "claude-opus-5.5", wantOK: true, wantProvider: "anthropic", wantID: "claude-opus-5-5"},
		{model: "Claude-Fable-5.2-High", wantOK: true, wantProvider: "anthropic", wantID: "claude-fable-5-2"},
		{model: "gpt-5.5-high", wantOK: true, wantProvider: "openai", wantID: "gpt-5.5"},
		{model: "gpt-5-5-xhigh", wantOK: true, wantProvider: "openai", wantID: "gpt-5.5"},
		{model: "o3-mini-low", wantOK: true, wantProvider: "openai", wantID: "o3-mini"},
		{model: "codex-mini-max", wantOK: true, wantProvider: "openai", wantID: "codex-mini"},
		{model: "gemini-4-hydrogen-thinking", wantOK: true, wantProvider: "google", wantID: "gemini-4-hydrogen"},
		{model: "grok-4-6-minimal", wantOK: true, wantProvider: "xai", wantID: "grok-4.6"},
		{model: "kimi-k3", wantOK: true, wantProvider: "moonshotai", wantID: "kimi-k3"},
		{model: "glm-5.3", wantOK: true, wantProvider: "zhipuai", wantID: "glm-5.3"},
		{model: "deepseek-v4-pro", wantOK: true, wantProvider: "deepseek", wantID: "deepseek-v4-pro"},
		{model: "qwen-3", wantOK: true, wantProvider: "alibaba", wantID: "qwen-3"},
		{model: "minimax-m2", wantOK: true, wantProvider: "minimax", wantID: "minimax-m2"},
		{model: "swe-2-medium", wantOK: false},
		{model: "compactor", wantOK: false},
		{model: "claude-sonnet-9", wantOK: false},
		{model: "", wantOK: false},
	}

	for _, tc := range cases {
		m, ok := snap.LookupVendor(tc.model)
		if ok != tc.wantOK {
			t.Errorf("LookupVendor(%q) ok = %v, want %v", tc.model, ok, tc.wantOK)
			continue
		}
		if ok && (m.ProviderID != tc.wantProvider || m.ID != tc.wantID) {
			t.Errorf("LookupVendor(%q) = %s/%s, want %s/%s", tc.model, m.ProviderID, m.ID, tc.wantProvider, tc.wantID)
		}
	}
}
