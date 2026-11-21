package l10n

import "testing"

func TestQuotaOnlyCatalogComplete(t *testing.T) {
	seen := map[Key]bool{}
	for _, key := range allKeys {
		seen[key] = true
	}
	for _, key := range append(append([]Key(nil), cursorKeys...), copilotKeys...) {
		seen[key] = true
	}
	for _, key := range quotaOnlyKeys {
		if seen[key] {
			t.Fatalf("duplicate key %s", key)
		}
		seen[key] = true
		for _, lang := range Languages() {
			if s, ok := quotaOnlyTables[lang][key]; !ok || isBlank(s) {
				t.Fatalf("missing %s/%s", lang, key)
			}
		}
	}
	for _, lang := range Languages() {
		if len(quotaOnlyTables[lang]) != len(quotaOnlyKeys) {
			t.Fatalf("%s has %d entries", lang, len(quotaOnlyTables[lang]))
		}
	}
}
