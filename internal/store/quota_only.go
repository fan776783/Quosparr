package store

// Droid and Devin are quota-only display providers like Cursor and Copilot:
// they never enter Providers, so accounts, CLI, session, Skill, MCP and
// pricing pages keep the base catalog.
const (
	ProviderDroid Provider = "droid"
	ProviderDevin Provider = "devin"
)

// QuotaOnlyPreferences are the opt-in switches of Droid and Devin. Their
// promoted fields keep their JSON names and are omitted while off.
type QuotaOnlyPreferences struct {
	DroidMonitoringEnabled bool `json:"droidMonitoringEnabled,omitempty"`
	DevinMonitoringEnabled bool `json:"devinMonitoringEnabled,omitempty"`
}

func readQuotaOnlyPreferences(r *reader) QuotaOnlyPreferences {
	return QuotaOnlyPreferences{
		DroidMonitoringEnabled: r.bool("droidMonitoringEnabled", false),
		DevinMonitoringEnabled: r.bool("devinMonitoringEnabled", false),
	}
}

// MonitoringEnabled reports whether a quota-only provider may read its
// login; the other providers are always on.
func (p Preferences) MonitoringEnabled(provider Provider) bool {
	switch provider {
	case ProviderDroid:
		return p.DroidMonitoringEnabled
	case ProviderDevin:
		return p.DevinMonitoringEnabled
	case ProviderCursor:
		return p.CursorMonitoringEnabled
	case ProviderCopilot:
		return p.CopilotMonitoringEnabled
	}
	return true
}
