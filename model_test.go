package main

import (
	"reflect"
	"testing"
)

func TestSplitLine(t *testing.T) {
	tests := []struct {
		in    string
		clean string
		codes []string
	}{
		{"Rigatonigratin  (20,20a,26,28)", "Rigatonigratin", []string{"20", "20a", "26", "28"}},
		{"Vinaigrette (25,29)", "Vinaigrette", []string{"25", "29"}},
		{"Salat", "Salat", nil},
		{"Köfta Fleischbällchen mit Weichkäse (20a,22,26)", "Köfta Fleischbällchen mit Weichkäse", []string{"20a", "22", "26"}},
		{"Kartoffel Selleriepfanne (27c,28)", "Kartoffel Selleriepfanne", []string{"27c", "28"}},
		// Brackets that are not a code list must survive untouched.
		{"Pommes frites (klein)", "Pommes frites (klein)", nil},
	}

	for _, tc := range tests {
		clean, codes := splitLine(tc.in)
		if clean != tc.clean {
			t.Errorf("splitLine(%q) text = %q, want %q", tc.in, clean, tc.clean)
		}
		if !reflect.DeepEqual(codes, tc.codes) {
			t.Errorf("splitLine(%q) codes = %v, want %v", tc.in, codes, tc.codes)
		}
	}
}

func TestSortCodes(t *testing.T) {
	// The two sources overlap and the upstream pads with spaces; both has to
	// come out as one sorted, deduplicated list.
	got := sortCodes([]string{"2", " 20", " 20a", "26", "20A", "", "9"})
	want := []string{"2", "9", "20", "20a", "26"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortCodes = %v, want %v", got, want)
	}
}

func TestIsoDate(t *testing.T) {
	if got := isoDate("09.09.2026"); got != "2026-09-09" {
		t.Errorf("isoDate = %q, want 2026-09-09", got)
	}
	if got := isoDate("2026-09-09"); got != "" {
		t.Errorf("isoDate of an already-ISO date = %q, want empty", got)
	}
}

func TestShortTime(t *testing.T) {
	if got := shortTime("11:30:00"); got != "11:30" {
		t.Errorf("shortTime = %q, want 11:30", got)
	}
	if got := shortTime(""); got != "" {
		t.Errorf("shortTime of empty = %q, want empty", got)
	}
}

func TestMealTags(t *testing.T) {
	tests := []struct {
		markers string
		climate string
		want    []string
	}{
		{"R,A", "B", []string{"beef", "animal-welfare"}},
		{"N,B", "A", []string{"vegan", "climate-plate"}},
		// A semicolon separator appears in a handful of upstream records.
		{"V;B", "", []string{"vegetarian"}},
		{"", "", []string{}},
		// Unknown markers are dropped rather than guessed at.
		{"B", "E", []string{}},
	}

	for _, tc := range tests {
		got := mealTags(rawMeal{Markers: tc.markers, Climate: tc.climate})
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("mealTags(%q, %q) = %v, want %v", tc.markers, tc.climate, got, tc.want)
		}
	}
}

func TestToMeal(t *testing.T) {
	raw := rawMeal{
		Number:       2889754,
		InternalName: "Rigatonigratin Rind",
		Date:         "09.09.2026",
		CategoryID:   101,
		Category:     "Menü 1 Mensa",
		Markers:      "R,A",
		Climate:      "B",
		Additives:    "2, 20, 20a, 25, 26, 28, 29",
		PriceStudent: 3.3,
		PriceStaff:   5.4,
		PriceGuest:   6.5,
		DE1:          "Rigatonigratin  (20,20a,26,28)",
		DE2:          "Salat",
		DE3:          "Vinaigrette (25,29)",
		EN1:          "Rigatoni gratin (20,20a,26,28)",
		EN2:          "salad",
	}

	meal := toMeal(raw)

	if meal.Name != "Rigatonigratin" {
		t.Errorf("Name = %q, want Rigatonigratin", meal.Name)
	}
	if meal.NameEn != "Rigatoni gratin" {
		t.Errorf("NameEn = %q, want Rigatoni gratin", meal.NameEn)
	}
	if want := []string{"Rigatonigratin", "Salat", "Vinaigrette"}; !reflect.DeepEqual(meal.Lines, want) {
		t.Errorf("Lines = %v, want %v", meal.Lines, want)
	}
	if want := []string{"beef", "animal-welfare"}; !reflect.DeepEqual(meal.Tags, want) {
		t.Errorf("Tags = %v, want %v", meal.Tags, want)
	}
	// Both sources of codes merged, deduplicated and sorted.
	if want := []string{"2", "20", "20a", "25", "26", "28", "29"}; !reflect.DeepEqual(meal.Additives, want) {
		t.Errorf("Additives = %v, want %v", meal.Additives, want)
	}
	if meal.Prices.Student != 3.3 {
		t.Errorf("Prices.Student = %v, want 3.3", meal.Prices.Student)
	}
}

func TestToMealFallsBackToInternalName(t *testing.T) {
	// Every description line empty: the kitchen's own name is all we have.
	meal := toMeal(rawMeal{Number: 1, InternalName: "Currywurst Menü", Date: "09.09.2026"})
	if meal.Name != "Currywurst Menü" {
		t.Errorf("Name = %q, want the internal name", meal.Name)
	}
	if len(meal.Lines) != 0 {
		t.Errorf("Lines = %v, want empty", meal.Lines)
	}
}

func TestGroupDays(t *testing.T) {
	meals := []rawMeal{
		{Number: 3, Date: "10.09.2026", CategoryID: 101, Category: "Menü 1 Mensa", DE1: "C"},
		{Number: 1, Date: "09.09.2026", CategoryID: 102, Category: "Menü 2 Mensa", DE1: "A"},
		{Number: 2, Date: "09.09.2026", CategoryID: 101, Category: "Menü 1 Mensa", DE1: "B"},
		{Number: 4, Date: "09.09.2026", CategoryID: 101, Category: "Menü 1 Mensa", DE1: "D"},
		{Number: 5, Date: "kaputt", CategoryID: 101, Category: "Menü 1 Mensa", DE1: "E"},
	}

	days := groupDays(meals)

	if len(days) != 2 {
		t.Fatalf("got %d days, want 2 (the unparsable date is dropped)", len(days))
	}
	if days[0].Date != "2026-09-09" || days[1].Date != "2026-09-10" {
		t.Errorf("days out of order: %s, %s", days[0].Date, days[1].Date)
	}
	if len(days[0].Categories) != 2 {
		t.Fatalf("got %d categories on day one, want 2", len(days[0].Categories))
	}
	if days[0].Categories[0].ID != 101 {
		t.Errorf("categories not sorted by id: got %d first", days[0].Categories[0].ID)
	}
	if meals := days[0].Categories[0].Meals; len(meals) != 2 || meals[0].ID != 2 {
		t.Errorf("meals within a category not sorted by id: %v", meals)
	}
}

func TestMergeCanteens(t *testing.T) {
	sites := []rawSite{
		{ID: 341, Name: "Hauptmensa inklKalteKüche"},
		{ID: 800, Name: "Kindertagesstätte"},
	}
	pages := []rawPage{
		newTestPage(54, "Hauptmensa", "hauptmensa", 341, "Vogelpothsweg 85, 44227 Dortmund"),
		// 453 exists on the website but is missing from /verbrauchsorte.
		newTestPage(60, "Mensa Max-Ophüls-Platz", "mensa-max-ophüls-platz", 453, "Max-Ophüls-Platz 2, 44137 Dortmund"),
		// A page without a restaurant id (FAQ, catering, ...) must be ignored.
		newTestPage(106, "FAQ", "faq", 0, ""),
	}

	canteens := mergeCanteens(sites, pages)

	if len(canteens) != 3 {
		t.Fatalf("got %d canteens, want 3", len(canteens))
	}
	byID := map[int]Canteen{}
	for _, c := range canteens {
		byID[c.ID] = c
	}
	if got := byID[341].Name; got != "Hauptmensa" {
		t.Errorf("CMS name should win over the kitchen name, got %q", got)
	}
	if got := byID[341].Address; got != "Vogelpothsweg 85, 44227 Dortmund" {
		t.Errorf("address = %q", got)
	}
	if _, ok := byID[453]; !ok {
		t.Error("canteen 453 is missing although the CMS knows it")
	}
	if got := byID[800].Name; got != "Kindertagesstätte" {
		t.Errorf("a canteen without a page keeps its upstream name, got %q", got)
	}
}

func newTestPage(id int, title, slug string, restaurantID int, location string) rawPage {
	var page rawPage
	page.ID = id
	page.Title = title
	page.Meta.Slug = slug
	page.Meta.HTMLURL = "https://www.stwdo.de/mensa-cafes-und-catering/" + slug + "/"

	block := struct {
		Type  string `json:"type"`
		Value struct {
			Location      string `json:"location"`
			Text          string `json:"text"`
			GoogleMapsURL string `json:"google_maps_url"`
			OpeningTimes  struct {
				RestaurantID flexInt `json:"restaurant_id"`
			} `json:"opening_times"`
		} `json:"value"`
	}{Type: "mensa_hero"}
	block.Value.Location = location
	block.Value.OpeningTimes.RestaurantID = flexInt(restaurantID)

	page.Intro = append(page.Intro, block)
	return page
}
