package l10n

import "testing"

func TestCursorCatalogStaysSeparateAndComplete(t *testing.T) {
	if len(cursorKeys) != 19 {
		t.Fatalf("unexpected Cursor key count: %d", len(cursorKeys))
	}
	seen := map[Key]bool{}
	for _, key := range cursorKeys {
		if seen[key] {
			t.Fatal("duplicate Cursor key")
		}
		seen[key] = true
		if _, ok := tables[English][key]; ok {
			t.Fatal("Cursor key leaked into upstream table")
		}
		for _, lang := range Languages() {
			value, ok := Lookup(lang, key)
			if !ok || isBlank(value) || New(lang).Text(key) != value {
				t.Fatalf("missing Cursor translation %s/%s", lang, key)
			}
		}
		if New(Language("unknown")).Text(key) != cursorTables[English][key] {
			t.Fatal("Cursor English fallback lost")
		}
	}
	if len(Keys()) != len(allKeys)+len(cursorKeys) {
		t.Fatal("complete key catalog lost extension keys")
	}
}
