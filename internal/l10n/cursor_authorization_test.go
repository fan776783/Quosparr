package l10n

import (
	"strings"
	"testing"
)

func TestCursorAuthorizationTranslations(t *testing.T) {
	for key, values := range cursorAuthorizationStrings {
		for i, lang := range Languages() {
			if strings.TrimSpace(values[i]) == "" || New(lang).CursorAuthorization(key) != values[i] {
				t.Fatalf("missing translation %s %s", key, lang)
			}
		}
	}
}
