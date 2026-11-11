package store

const ProviderCopilot Provider = "copilot"

type CopilotPreferences struct {
	CopilotMonitoringEnabled bool   `json:"copilotMonitoringEnabled,omitempty"`
	CopilotSource            string `json:"copilotSource,omitempty"`
	CopilotAccountID         string `json:"copilotAccountID,omitempty"`
}

func readCopilotPreferences(r *reader) CopilotPreferences {
	source, _ := r.string("copilotSource")
	account, _ := r.string("copilotAccountID")
	return CopilotPreferences{CopilotMonitoringEnabled: r.bool("copilotMonitoringEnabled", false), CopilotSource: source, CopilotAccountID: account}
}
func (p Preferences) CopilotCredentialSource() string {
	switch p.CopilotSource {
	case "vscode", "vscode-insiders":
		return p.CopilotSource
	default:
		return "cli"
	}
}
func (p Provider) QuotaOnly() bool { return p == ProviderCursor || p == ProviderCopilot }
