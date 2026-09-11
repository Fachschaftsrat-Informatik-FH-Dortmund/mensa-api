package main

// The legends below are not part of the Speiseplan API. The additive and
// allergen texts come from the CMS page
// https://www.stwdo.de/mensa-cafes-und-catering/allgemein/zusatzstoffe/,
// the marker names from the canteen page's own JavaScript. Both were read on
// 2026-09-09; if the canteen ever adds a code, it shows up unresolved in
// /legend rather than getting lost.

// LegendEntry is one resolvable code.
type LegendEntry struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// markerTags maps the upstream FREIKENNZEICHEN letters to stable English
// slugs. "B" appears often in the data but has no documented meaning and is
// ignored by the canteen's own website, so we pass it through as "b" rather
// than inventing a label for it.
var markerTags = map[string]string{
	"V": "vegetarian",
	"N": "vegan",
	"G": "poultry",
	"S": "pork",
	"R": "beef",
	"L": "lamb",
	"W": "game",
	"F": "fish",
	"A": "animal-welfare",
}

// tagLabels gives every tag we emit a German label for display.
var tagLabels = map[string]string{
	"vegetarian":     "Vegetarisch",
	"vegan":          "Vegan",
	"poultry":        "Geflügel",
	"pork":           "Schwein",
	"beef":           "Rind",
	"lamb":           "Lamm",
	"game":           "Wild",
	"fish":           "Fisch",
	"animal-welfare": "Artgerecht",
	"climate-plate":  "Klimateller",
}

// tagOrder keeps the tags of a meal in the order the canteen's own legend
// uses, so two clients never disagree about the sorting.
var tagOrder = []string{
	"vegetarian", "vegan", "poultry", "pork", "beef",
	"lamb", "game", "fish", "animal-welfare", "climate-plate",
}

var additiveLegend = []LegendEntry{
	{"1", "mit Antioxidationsmittel"},
	{"2", "mit Konservierungsstoff"},
	{"3", "geschwefelt"},
	{"4", "mit Farbstoff"},
	{"5", "gewachst"},
	{"6", "mit Geschmacksverstärker"},
	{"7", "mit Süßungsmittel"},
	{"8", "enthält eine Phenylalaninquelle"},
	{"9", "mit Phosphat"},
	{"10", "geschwärzt"},
	{"11", "mit Alkohol"},
}

// allergenLegend uses the canteen's numbering. Everything except code 31 also
// covers products made from it.
var allergenLegend = []LegendEntry{
	{"20", "Gluten (nicht näher bezeichnet)"},
	{"20a", "Gluten aus Weizen"},
	{"20b", "Gluten aus Roggen"},
	{"20c", "Gluten aus Gerste"},
	{"20d", "Gluten aus Hafer"},
	{"20e", "Gluten aus Dinkel"},
	{"20f", "Gluten aus Kamut"},
	{"21", "Krebstiere"},
	{"22", "Eier"},
	{"23", "Fisch"},
	{"24", "Erdnüsse"},
	{"25", "Soja"},
	{"26", "Milch inkl. Lactose"},
	{"27a", "Mandeln"},
	{"27b", "Haselnüsse"},
	{"27c", "Walnüsse"},
	{"27d", "Kaschunüsse"},
	{"27e", "Pekannüsse"},
	{"27f", "Paranüsse"},
	{"27g", "Pistazien"},
	{"27h", "Macadamia- oder Queenslandnüsse"},
	{"28", "Sellerie"},
	{"29", "Senf"},
	{"30", "Sesamsamen"},
	{"31", "Schwefeldioxid/Sulfite > 10 mg/kg"},
	{"32", "Lupine"},
	{"33", "Weichtiere"},
}

// climateLegend explains the CO2 class. The canteen's own frontend only shows
// the climate-plate icon for class A, so that is the reading we adopt.
var climateLegend = []LegendEntry{
	{"A", "Klimateller (beste CO₂-Klasse)"},
	{"B", "CO₂-Klasse B"},
	{"C", "CO₂-Klasse C"},
	{"E", "CO₂-Klasse E"},
}

// Legend is what GET /legend returns.
type Legend struct {
	Tags      []LegendEntry `json:"tags"`
	Additives []LegendEntry `json:"additives"`
	Allergens []LegendEntry `json:"allergens"`
	Climate   []LegendEntry `json:"climate"`
	Note      string        `json:"note"`
}

func buildLegend() Legend {
	tags := make([]LegendEntry, 0, len(tagOrder))
	for _, slug := range tagOrder {
		tags = append(tags, LegendEntry{Code: slug, Label: tagLabels[slug]})
	}
	return Legend{
		Tags:      tags,
		Additives: additiveLegend,
		Allergens: allergenLegend,
		Climate:   climateLegend,
		Note:      "Allergene gelten jeweils auch für Erzeugnisse daraus. Die Legende stammt von der Stwdo-Website, nicht aus deren API.",
	}
}
