package store

import (
	"slices"
	"testing"
)

func TestCopilotPreferencesDisplayOnlyRoundTrip(t *testing.T) {
	p := DefaultPreferences()
	p.CopilotMonitoringEnabled = true
	p.CopilotSource = "vscode"
	p.CopilotAccountID = "7"
	p.SelectedProvider = ProviderCopilot
	p.PanelProviders = []Provider{ProviderCopilot}
	p.SetStatusBarGroups([][]Provider{{ProviderCopilot}})
	p.FloatingWindow.Providers = []Provider{ProviderCopilot}
	data, e := EncodePreferences(p)
	if e != nil {
		t.Fatal(e)
	}
	back, e := DecodePreferences(data)
	if e != nil || !back.CopilotMonitoringEnabled || back.CopilotSource != "vscode" || back.CopilotAccountID != "7" || back.SelectedProvider != ProviderCopilot || !slices.Equal(back.DisplayPanelSelection(), []Provider{ProviderCopilot}) {
		t.Fatal("Copilot preferences did not round trip")
	}
	if slices.Contains(Providers(), ProviderCopilot) {
		t.Fatal("Copilot entered CLI management catalog")
	}
}
