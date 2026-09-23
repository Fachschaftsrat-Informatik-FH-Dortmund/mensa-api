package main

import "testing"

// Every code in /legend has to carry an English label, so a code added later
// without a translation fails here instead of silently falling back to German.
func TestLegendHasEnglishLabels(t *testing.T) {
	l := buildLegend()
	lists := map[string][]LegendEntry{
		"tags":      l.Tags,
		"additives": l.Additives,
		"allergens": l.Allergens,
		"climate":   l.Climate,
	}
	for name, entries := range lists {
		for _, e := range entries {
			if e.Label == "" || e.LabelEn == "" {
				t.Errorf("%s %q: label = %q, labelEn = %q", name, e.Code, e.Label, e.LabelEn)
			}
		}
	}
	if l.NoteEn == "" {
		t.Error("noteEn is empty")
	}
}
