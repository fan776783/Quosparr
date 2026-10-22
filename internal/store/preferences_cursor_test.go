package store

import (
	"bytes"
	"reflect"
	"testing"
)

func TestCursorOptInDefaultsAndProviderLimits(t *testing.T) {
	p := DefaultPreferences()
	data, err := EncodePreferences(p)
	if err != nil || p.CursorMonitoringEnabled || bytes.Contains(data, []byte("cursorMonitoringEnabled")) {
		t.Fatal("Cursor must default off without changing frozen defaults")
	}
	p.CursorMonitoringEnabled = true
	p.SelectedProvider = ProviderCursor
	p.StatusBarProviderScope = ProviderScopeCustom
	p.StatusBarProviders = []Provider{ProviderCursor, ProviderClaude}
	p.FloatingWindow.Providers = []Provider{ProviderCursor, ProviderCodex, ProviderGrok, ProviderClaude}
	data, err = EncodePreferences(p)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodePreferences(data)
	if err != nil {
		t.Fatal(err)
	}
	if !back.CursorMonitoringEnabled || back.SelectedProvider != ProviderCursor || len(back.StatusBarSelection()) != 2 || len(back.FloatingSelection()) != 3 {
		t.Fatal("Cursor preference or display bounds lost")
	}
}

func TestCursorStatusBarGroupsRoundTripWithLegacyPair(t *testing.T) {
	p := DefaultPreferences()
	groups := [][]Provider{{ProviderCodex, ProviderClaude}, {ProviderCursor}, {ProviderGrok}}
	p.SetStatusBarGroups(groups)
	data, err := EncodePreferences(p)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodePreferences(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.StatusBarGroups(), groups) || !reflect.DeepEqual(back.StatusBarSelection(), []Provider{ProviderCodex, ProviderClaude}) {
		t.Fatalf("groups or legacy pair lost: %+v", back.StatusBar)
	}
}
