package store

const ProviderCursor Provider = "cursor"

// CursorPreferences extends persisted preferences without adding fields to the
// frozen upstream preferences contract. Its promoted fields keep JSON names.
type CursorPreferences struct {
	CursorMonitoringEnabled     bool `json:"cursorMonitoringEnabled,omitempty"`
	CursorRememberAuthorization bool `json:"cursorRememberAuthorization,omitempty"`
}

func readCursorPreferences(r *reader) CursorPreferences {
	return CursorPreferences{
		CursorMonitoringEnabled:     r.bool("cursorMonitoringEnabled", false),
		CursorRememberAuthorization: r.bool("cursorRememberAuthorization", false),
	}
}

// DisplayProviders includes quota-only providers. Account, CLI, session, Skill,
// MCP and pricing operations must continue to use Providers.
func DisplayProviders() []Provider { return append(Providers(), ProviderCursor) }

func canonicalDisplayProviders(in []Provider, limit int) []Provider {
	seen := make(map[Provider]bool, len(in))
	for _, p := range in {
		if p.Valid() {
			seen[p] = true
		}
	}
	out := make([]Provider, 0, limit)
	for _, p := range DisplayProviders() {
		if seen[p] {
			out = append(out, p)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

// DisplayPanelSelection adds the fork's quota-only provider without changing
// PanelSelection, which upstream callers and tests use for the base catalog.
func (p Preferences) DisplayPanelSelection() []Provider {
	if sel := canonicalDisplayProviders(p.PanelProviders, len(DisplayProviders())); len(sel) > 0 {
		return sel
	}
	return DisplayProviders()
}
