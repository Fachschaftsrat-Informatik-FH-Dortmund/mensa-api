package main

// The legends below are not part of the Speiseplan API. The additive and
// allergen texts come from the CMS page
// https://www.stwdo.de/mensa-cafes-und-catering/allgemein/zusatzstoffe/,
// the marker names from the canteen page's own JavaScript. Both were read on
// 2026-09-09; if the canteen ever adds a code, it shows up unresolved in
// /legend rather than getting lost.
//
// The Stwdo publishes no English version, so the English labels are our own.
// Allergens (20–33) and code 8 follow the official English wording of
// Regulation (EU) No 1169/2011, Annexes II and III, which is why the spelling
// is British throughout. The other additives are German national labels
// without an official translation. The German label stays authoritative.

// LegendEntry is one resolvable code.
type LegendEntry struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	LabelEn string `json:"labelEn,omitempty"`
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

// tagLabels gives every tag we emit a German and an English label for
// display. "Klimateller" is the Stwdo's own name and stays untranslated.
var tagLabels = map[string]struct{ de, en string }{
	"vegetarian":     {"Vegetarisch", "Vegetarian"},
	"vegan":          {"Vegan", "Vegan"},
	"poultry":        {"Geflügel", "Poultry"},
	"pork":           {"Schwein", "Pork"},
	"beef":           {"Rind", "Beef"},
	"lamb":           {"Lamm", "Lamb"},
	"game":           {"Wild", "Game"},
	"fish":           {"Fisch", "Fish"},
	"animal-welfare": {"Artgerecht", "Animal welfare"},
	"climate-plate":  {"Klimateller", "Klimateller (climate plate)"},
}

// tagOrder keeps the tags of a meal in the order the canteen's own legend
// uses, so two clients never disagree about the sorting.
var tagOrder = []string{
	"vegetarian", "vegan", "poultry", "pork", "beef",
	"lamb", "game", "fish", "animal-welfare", "climate-plate",
}

var additiveLegend = []LegendEntry{
	{"1", "mit Antioxidationsmittel", "with antioxidant"},
	{"2", "mit Konservierungsstoff", "with preservative"},
	{"3", "geschwefelt", "sulphured"},
	{"4", "mit Farbstoff", "with colouring"},
	{"5", "gewachst", "waxed"},
	{"6", "mit Geschmacksverstärker", "with flavour enhancer"},
	{"7", "mit Süßungsmittel", "with sweetener"},
	{"8", "enthält eine Phenylalaninquelle", "contains a source of phenylalanine"},
	{"9", "mit Phosphat", "with phosphate"},
	{"10", "geschwärzt", "blackened"},
	{"11", "mit Alkohol", "with alcohol"},
}

// allergenLegend uses the canteen's numbering. Everything except code 31 also
// covers products made from it.
var allergenLegend = []LegendEntry{
	{"20", "Gluten (nicht näher bezeichnet)", "Gluten (unspecified)"},
	{"20a", "Gluten aus Weizen", "Gluten from wheat"},
	{"20b", "Gluten aus Roggen", "Gluten from rye"},
	{"20c", "Gluten aus Gerste", "Gluten from barley"},
	{"20d", "Gluten aus Hafer", "Gluten from oats"},
	{"20e", "Gluten aus Dinkel", "Gluten from spelt"},
	{"20f", "Gluten aus Kamut", "Gluten from khorasan wheat"},
	{"21", "Krebstiere", "Crustaceans"},
	{"22", "Eier", "Eggs"},
	{"23", "Fisch", "Fish"},
	{"24", "Erdnüsse", "Peanuts"},
	{"25", "Soja", "Soybeans"},
	{"26", "Milch inkl. Lactose", "Milk (including lactose)"},
	{"27a", "Mandeln", "Almonds"},
	{"27b", "Haselnüsse", "Hazelnuts"},
	{"27c", "Walnüsse", "Walnuts"},
	{"27d", "Kaschunüsse", "Cashews"},
	{"27e", "Pekannüsse", "Pecan nuts"},
	{"27f", "Paranüsse", "Brazil nuts"},
	{"27g", "Pistazien", "Pistachio nuts"},
	{"27h", "Macadamia- oder Queenslandnüsse", "Macadamia or Queensland nuts"},
	{"28", "Sellerie", "Celery"},
	{"29", "Senf", "Mustard"},
	{"30", "Sesamsamen", "Sesame seeds"},
	{"31", "Schwefeldioxid/Sulfite > 10 mg/kg", "Sulphur dioxide/sulphites > 10 mg/kg"},
	{"32", "Lupine", "Lupin"},
	{"33", "Weichtiere", "Molluscs"},
}

// climateLegend explains the CO2 class. The canteen's own frontend only shows
// the climate-plate icon for class A, so that is the reading we adopt.
var climateLegend = []LegendEntry{
	{"A", "Klimateller (beste CO₂-Klasse)", "Klimateller (best CO₂ class)"},
	{"B", "CO₂-Klasse B", "CO₂ class B"},
	{"C", "CO₂-Klasse C", "CO₂ class C"},
	{"E", "CO₂-Klasse E", "CO₂ class E"},
}

// Legend is what GET /legend returns.
type Legend struct {
	Tags      []LegendEntry `json:"tags"`
	Additives []LegendEntry `json:"additives"`
	Allergens []LegendEntry `json:"allergens"`
	Climate   []LegendEntry `json:"climate"`
	Note      string        `json:"note"`
	NoteEn    string        `json:"noteEn,omitempty"`
}

func buildLegend() Legend {
	tags := make([]LegendEntry, 0, len(tagOrder))
	for _, slug := range tagOrder {
		tags = append(tags, LegendEntry{Code: slug, Label: tagLabels[slug].de, LabelEn: tagLabels[slug].en})
	}
	return Legend{
		Tags:      tags,
		Additives: additiveLegend,
		Allergens: allergenLegend,
		Climate:   climateLegend,
		Note:      "Allergene gelten jeweils auch für Erzeugnisse daraus. Die Legende stammt von der Stwdo-Website, nicht aus deren API.",
		NoteEn:    "Allergens also cover products made from them. The legend comes from the Stwdo website, not from its API. The English labels are an unofficial translation; the German label is authoritative.",
	}
}
