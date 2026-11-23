package pricing

import "strings"

var vendorPrefixes = []struct {
	prefix   string
	provider string
}{
	{prefix: "claude", provider: "anthropic"},
	{prefix: "gpt-", provider: "openai"},
	{prefix: "o1", provider: "openai"},
	{prefix: "o3", provider: "openai"},
	{prefix: "o4", provider: "openai"},
	{prefix: "codex", provider: "openai"},
	{prefix: "gemini", provider: "google"},
	{prefix: "grok", provider: "xai"},
	{prefix: "kimi", provider: "moonshotai"},
	{prefix: "glm", provider: "zhipuai"},
	{prefix: "deepseek", provider: "deepseek"},
	{prefix: "qwen", provider: "alibaba"},
	{prefix: "minimax", provider: "minimax"},
}

var reasoningEffortSuffixes = []string{
	"-minimal",
	"-low",
	"-medium",
	"-high",
	"-xhigh",
	"-max",
	"-thinking",
}

// LookupVendor infers the official vendor provider ID from model's prefix and
// searches only that provider in the snapshot. Candidates are tried in order:
// original, lowercase, without a reasoning-effort suffix, and with digit-dot /
// digit-hyphen separators swapped (e.g. 5.5 <-> 5-5).
func (s Snapshot) LookupVendor(model string) (Model, bool) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return Model{}, false
	}
	lower := strings.ToLower(trimmed)
	provider := vendorProvider(lower)
	if provider == "" {
		return Model{}, false
	}
	for _, candidate := range vendorCandidates(trimmed, lower) {
		if m, ok := s.Lookup(provider, candidate); ok {
			return m, true
		}
	}
	return Model{}, false
}

func vendorProvider(lowerModel string) string {
	for _, rule := range vendorPrefixes {
		if strings.HasPrefix(lowerModel, rule.prefix) {
			return rule.provider
		}
	}
	return ""
}

func vendorCandidates(trimmed, lower string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	add(trimmed)
	add(lower)
	stripped := stripReasoningSuffix(lower)
	add(stripped)
	for _, base := range []string{trimmed, lower, stripped} {
		if base == "" {
			continue
		}
		add(swapDigitSep(base, '.', '-'))
		add(swapDigitSep(base, '-', '.'))
	}
	return out
}

func stripReasoningSuffix(lower string) string {
	for _, suffix := range reasoningEffortSuffixes {
		if base, ok := strings.CutSuffix(lower, suffix); ok && base != "" {
			return base
		}
	}
	return ""
}

func swapDigitSep(s string, from, to byte) string {
	b := []byte(s)
	changed := false
	for i := 1; i+1 < len(b); i++ {
		if b[i] == from && isDigit(b[i-1]) && isDigit(b[i+1]) {
			b[i] = to
			changed = true
		}
	}
	if !changed {
		return ""
	}
	return string(b)
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
