package l10n

import "testing"

func TestCopilotCatalogComplete(t *testing.T) {
	for _, key := range copilotKeys {
		for _, lang := range Languages() {
			s, ok := Lookup(lang, key)
			if !ok || isBlank(s) {
				t.Fatalf("missing %s/%s", lang, key)
			}
		}
	}
}
