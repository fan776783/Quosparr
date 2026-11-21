package l10n

// forkCatalog is one of the fork's own key tables, kept out of the frozen
// upstream tables and allKeys.
type forkCatalog struct {
	tables map[Language]map[Key]string
	keys   []Key
}

// forkCatalogs are the fork's tables, consulted after the upstream ones; a
// new fork catalog is added here only. Package initialization fills the
// tables first, as this list depends on them.
var forkCatalogs = []forkCatalog{
	{cursorTables, cursorKeys},
	{copilotTables, copilotKeys},
	{quotaOnlyTables, quotaOnlyKeys},
}

func forkLookup(lang Language, key Key) (string, bool) {
	for _, c := range forkCatalogs {
		if s, ok := c.tables[lang][key]; ok {
			return s, true
		}
	}
	return "", false
}

func forkKeys() []Key {
	var out []Key
	for _, c := range forkCatalogs {
		out = append(out, c.keys...)
	}
	return out
}
