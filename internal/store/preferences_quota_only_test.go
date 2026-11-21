package store

import (
	"slices"
	"strings"
	"testing"
)

func TestQuotaOnlyPreferencesRoundTrip(t *testing.T) {
	p := DefaultPreferences()
	if p.MonitoringEnabled(ProviderDroid) || p.MonitoringEnabled(ProviderDevin) || !p.MonitoringEnabled(ProviderCodex) {
		t.Fatal("Droid and Devin must be opt-in")
	}
	p.DroidMonitoringEnabled, p.DevinMonitoringEnabled = true, true
	p.SelectedProvider = ProviderDevin
	p.PanelProviders = []Provider{ProviderDevin, ProviderDroid}
	data, err := EncodePreferences(p)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodePreferences(data)
	if err != nil || !back.DroidMonitoringEnabled || !back.DevinMonitoringEnabled || back.SelectedProvider != ProviderDevin ||
		!slices.Equal(back.DisplayPanelSelection(), []Provider{ProviderDroid, ProviderDevin}) {
		t.Fatalf("round trip: %+v %v", back.QuotaOnlyPreferences, err)
	}
	for _, provider := range []Provider{ProviderDroid, ProviderDevin} {
		if slices.Contains(Providers(), provider) || !provider.QuotaOnly() || !slices.Contains(DisplayProviders(), provider) {
			t.Fatalf("%s catalog membership", provider)
		}
	}
	off, _ := EncodePreferences(DefaultPreferences())
	if strings.Contains(string(off), "droidMonitoringEnabled") || strings.Contains(string(off), "devinMonitoringEnabled") {
		t.Fatal("default preferences write the switch")
	}
}
