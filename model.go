package main

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The types in this file are what we serve. They are deliberately not the
// upstream's shape: names are lowercase and English, dates are ISO, the seven
// description lines became one slice, and the operational fields (kitchen
// notes, till index, monitor slot, the ten permanently empty nutrition
// fields) are gone.

// CanteenRef is the short form used inside menu responses.
type CanteenRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Canteen is one location with everything we know about it.
type Canteen struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	Address     string `json:"address,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	MapsURL     string `json:"mapsUrl,omitempty"`
	HasMenu     bool   `json:"hasMenu"`
}

func (c Canteen) ref() CanteenRef {
	return CanteenRef{ID: c.ID, Name: c.Name}
}

// Prices are in euro. The upstream also has a pupil price, but it is 0.00 for
// 93 % of all meals and 0.50 for the rest, so it carries no information.
type Prices struct {
	Student float64 `json:"student"`
	Staff   float64 `json:"staff"`
	Guest   float64 `json:"guest"`
}

// Meal is one dish on one day.
type Meal struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	NameEn    string   `json:"nameEn,omitempty"`
	Lines     []string `json:"lines"`
	LinesEn   []string `json:"linesEn,omitempty"`
	Prices    Prices   `json:"prices"`
	Tags      []string `json:"tags"`
	Additives []string `json:"additives"`
	CO2Class  string   `json:"co2Class,omitempty"`
}

// Category groups the meals of one serving counter, e.g. "Menü 1 Mensa".
type Category struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Meals []Meal `json:"meals"`
}

// Day is one date's offer at one canteen.
type Day struct {
	Date       string     `json:"date"` // ISO, YYYY-MM-DD
	Categories []Category `json:"categories"`
}

// MenuResponse is GET /canteens/{id}/menu.
type MenuResponse struct {
	Canteen CanteenRef `json:"canteen"`
	Days    []Day      `json:"days"`
}

// DayResponse is GET /canteens/{id}/menu/{date}.
type DayResponse struct {
	Canteen    CanteenRef `json:"canteen"`
	Date       string     `json:"date"`
	Categories []Category `json:"categories"`
}

// --- meals -----------------------------------------------------------------

// codeGroup matches an additive list in brackets, as the upstream writes it
// into the description text: "Rigatonigratin (20,20a,26,28)".
var codeGroup = regexp.MustCompile(`\s*\(\s*\d{1,2}[a-z]?(?:\s*,\s*\d{1,2}[a-z]?)*\s*\)`)

var multiSpace = regexp.MustCompile(`\s{2,}`)

// splitLine separates a description line from the additive codes baked into
// it and returns both.
func splitLine(line string) (string, []string) {
	var codes []string
	for _, group := range codeGroup.FindAllString(line, -1) {
		trimmed := strings.Trim(strings.TrimSpace(group), "()")
		for _, code := range strings.Split(trimmed, ",") {
			if code = strings.TrimSpace(code); code != "" {
				codes = append(codes, code)
			}
		}
	}
	clean := multiSpace.ReplaceAllString(codeGroup.ReplaceAllString(line, ""), " ")
	return strings.TrimSpace(clean), codes
}

// cleanLines strips the codes from every line and collects them on the side.
func cleanLines(lines []string) ([]string, []string) {
	out := make([]string, 0, len(lines))
	var codes []string
	for _, line := range lines {
		clean, found := splitLine(line)
		if clean != "" {
			out = append(out, clean)
		}
		codes = append(codes, found...)
	}
	return out, codes
}

// splitMarkers cuts FREIKENNZEICHEN apart. The upstream uses a comma most of
// the time but a semicolon in a handful of records.
func splitMarkers(s string) []string {
	return strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool {
		return r == ',' || r == ';'
	})
}

// mealTags turns the marker letters and the CO2 class into stable slugs.
func mealTags(m rawMeal) []string {
	seen := map[string]bool{}
	for _, marker := range splitMarkers(m.Markers) {
		marker = strings.TrimSpace(marker)
		if marker == "" {
			continue
		}
		if tag, ok := markerTags[marker]; ok {
			seen[tag] = true
		}
	}
	if strings.EqualFold(strings.TrimSpace(m.Climate), "A") {
		seen["climate-plate"] = true
	}

	tags := make([]string, 0, len(seen))
	for _, tag := range tagOrder {
		if seen[tag] {
			tags = append(tags, tag)
		}
	}
	return tags
}

// sortCodes orders additive codes the way a legend does: 1, 2, 20, 20a, 20b.
func sortCodes(codes []string) []string {
	unique := map[string]bool{}
	for _, code := range codes {
		// ZUSATZSTOFFNUMMERN arrives as "2, 20, 20a" — with the spaces.
		if code = strings.ToLower(strings.TrimSpace(code)); code != "" {
			unique[code] = true
		}
	}
	out := make([]string, 0, len(unique))
	for code := range unique {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool {
		ni, si := splitCode(out[i])
		nj, sj := splitCode(out[j])
		if ni != nj {
			return ni < nj
		}
		return si < sj
	})
	return out
}

// splitCode cuts "20a" into 20 and "a" so codes sort numerically.
func splitCode(code string) (int, string) {
	digits := 0
	for digits < len(code) && code[digits] >= '0' && code[digits] <= '9' {
		digits++
	}
	n, _ := strconv.Atoi(code[:digits])
	return n, code[digits:]
}

// toMeal is the whole point of this service: 45 upstream fields in, ten
// useful ones out.
func toMeal(m rawMeal) Meal {
	lines, codesDE := cleanLines(m.germanLines())
	linesEn, _ := cleanLines(m.englishLines())

	// ZUSATZSTOFFNUMMERN is empty for about one meal in eight, so fall back to
	// the codes we found in the text.
	codes := append(strings.Split(m.Additives, ","), codesDE...)

	meal := Meal{
		ID:        m.Number,
		Lines:     lines,
		LinesEn:   linesEn,
		Prices:    Prices{Student: m.PriceStudent, Staff: m.PriceStaff, Guest: m.PriceGuest},
		Tags:      mealTags(m),
		Additives: sortCodes(codes),
		CO2Class:  strings.TrimSpace(m.Climate),
	}

	if len(lines) > 0 {
		meal.Name = lines[0]
	} else {
		// Nothing to show the guest; the kitchen's internal name is all we have.
		meal.Name = strings.TrimSpace(m.InternalName)
	}
	if len(linesEn) > 0 {
		meal.NameEn = linesEn[0]
	}
	return meal
}

// groupDays turns a flat list of upstream articles into days and categories.
func groupDays(meals []rawMeal) []Day {
	catNames := map[int]string{}
	byDay := map[string]map[int][]Meal{}

	for _, raw := range meals {
		date := isoDate(raw.Date)
		if date == "" {
			continue
		}
		if _, ok := byDay[date]; !ok {
			byDay[date] = map[int][]Meal{}
		}
		catNames[raw.CategoryID] = raw.Category
		byDay[date][raw.CategoryID] = append(byDay[date][raw.CategoryID], toMeal(raw))
	}

	dates := make([]string, 0, len(byDay))
	for date := range byDay {
		dates = append(dates, date)
	}
	slices.Sort(dates)

	days := make([]Day, 0, len(dates))
	for _, date := range dates {
		days = append(days, Day{Date: date, Categories: buildCategories(byDay[date], catNames)})
	}
	return days
}

func buildCategories(perCategory map[int][]Meal, names map[int]string) []Category {
	ids := make([]int, 0, len(perCategory))
	for id := range perCategory {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	categories := make([]Category, 0, len(ids))
	for _, id := range ids {
		meals := perCategory[id]
		slices.SortFunc(meals, func(a, b Meal) int { return a.ID - b.ID })
		categories = append(categories, Category{ID: id, Name: names[id], Meals: meals})
	}
	return categories
}

// --- opening hours ---------------------------------------------------------

// Schedule is one day's opening times. Serving times are only emitted when
// they actually differ from the regular ones.
type Schedule struct {
	IsOpen       bool   `json:"isOpen"`
	Open         string `json:"open,omitempty"`
	Close        string `json:"close,omitempty"`
	ServingOpen  string `json:"servingOpen,omitempty"`
	ServingClose string `json:"servingClose,omitempty"`
}

// DaySchedule is one dated day of the forecast.
type DaySchedule struct {
	Date    string `json:"date"`
	Weekday string `json:"weekday"`
	Schedule
	ClosedReason string `json:"closedReason,omitempty"`
}

// WeekdaySchedule is one entry of the regular weekly pattern.
type WeekdaySchedule struct {
	Weekday string `json:"weekday"`
	Schedule
}

// Closure is a day the canteen stays shut.
type Closure struct {
	Label     string `json:"label"`
	Date      string `json:"date,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	IsHoliday bool   `json:"isHoliday"`
	Scope     string `json:"scope"` // "global" or "canteen"
}

// Today is the answer to "is it open right now".
type Today struct {
	Date   string `json:"date"`
	IsOpen bool   `json:"isOpen"`
	Reason string `json:"reason,omitempty"`
}

// Hours is GET /canteens/{id}/hours.
type Hours struct {
	Canteen  CanteenRef        `json:"canteen"`
	Today    Today             `json:"today"`
	Forecast []DaySchedule     `json:"forecast"`
	Week     []WeekdaySchedule `json:"week"`
	Closures []Closure         `json:"closures"`
}

var weekOrder = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func toSchedule(s rawSchedule) Schedule {
	out := Schedule{
		IsOpen: s.IsOpen,
		Open:   shortTime(s.Regular.Open),
		Close:  shortTime(s.Regular.Close),
	}
	if s.ServingDiffer {
		out.ServingOpen = shortTime(s.Serving.Open)
		out.ServingClose = shortTime(s.Serving.Close)
	}
	return out
}

func toHours(ref CanteenRef, raw *rawHours) Hours {
	h := Hours{
		Canteen: ref,
		Today:   Today{Date: raw.Today.Date, IsOpen: raw.Today.IsOpen, Reason: raw.Today.Reason},
	}

	for _, day := range raw.Forecast {
		h.Forecast = append(h.Forecast, DaySchedule{
			Date:         day.Date,
			Weekday:      day.Weekday,
			Schedule:     toSchedule(day),
			ClosedReason: day.ClosedReason,
		})
	}

	// The upstream splits the weekly pattern into "weekdays" and "weekend";
	// for a client that is one table.
	for _, name := range weekOrder {
		day, ok := raw.Weekdays[name]
		if !ok {
			if day, ok = raw.Weekend[name]; !ok {
				continue
			}
		}
		h.Week = append(h.Week, WeekdaySchedule{Weekday: name, Schedule: toSchedule(day)})
	}

	for _, c := range raw.Closures.Global {
		h.Closures = append(h.Closures, toClosure(c, "global"))
	}
	for _, c := range raw.Closures.Restaurant {
		h.Closures = append(h.Closures, toClosure(c, "canteen"))
	}
	return h
}

func toClosure(c rawClosure, scope string) Closure {
	return Closure{
		Label:     c.Label,
		Date:      c.Date,
		From:      c.DateFrom,
		To:        c.DateTo,
		IsHoliday: c.IsHoliday,
		Scope:     scope,
	}
}

// --- helpers ---------------------------------------------------------------

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// isoDate turns the upstream's DD.MM.YYYY into YYYY-MM-DD, so dates sort and
// compare as plain strings.
func isoDate(german string) string {
	parts := strings.Split(strings.TrimSpace(german), ".")
	if len(parts) != 3 || len(parts[0]) != 2 || len(parts[1]) != 2 || len(parts[2]) != 4 {
		return ""
	}
	return parts[2] + "-" + parts[1] + "-" + parts[0]
}

// shortTime drops the pointless seconds from "11:30:00".
func shortTime(t string) string {
	if len(t) == 8 && t[2] == ':' && t[5] == ':' {
		return t[:5]
	}
	return t
}
