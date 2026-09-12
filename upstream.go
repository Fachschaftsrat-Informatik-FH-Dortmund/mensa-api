package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	upstreamBase = "https://www.stwdo.de/api/v2"

	// version is also what openapi.json reports; TestVersionsAgree keeps the
	// two from drifting apart.
	version   = "0.2.0"
	userAgent = "mensa-api/" + version +
		" (caching proxy; https://github.com/Fachschaftsrat-Informatik-FH-Dortmund/mensa-api)"

	// The upstream caps limit at 300 without saying so. Paging with skip is
	// the only way to see everything a busy location offers in two weeks.
	pageSize = 300

	// Safety net for allMeals: today's whole two-week offer fits in four
	// pages, so anything near this means the upstream stopped shortening the
	// last page and we would otherwise loop for ever.
	maxPages = 20
)

// client talks to stwdo.de. It is deliberately thin: every method returns the
// upstream payload reduced to the fields we actually serve.
type client struct {
	http  *http.Client
	delay time.Duration // pause between requests, to stay a polite guest
}

func newClient(timeout, delay time.Duration) *client {
	return &client{
		http:  &http.Client{Timeout: timeout},
		delay: delay,
	}
}

// get decodes a single upstream response into out.
func (c *client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := upstreamBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GET %s: decode: %w", u, err)
	}
	return nil
}

// pause spaces out consecutive upstream requests. Only the loops that need
// several calls in a row use it — a single fetch with a guest waiting on it
// must not sit idle for no reason. The delay is jittered for the same reason
// the cache deadlines are: a fixed 200 ms between calls is a fingerprint.
func (c *client) pause() {
	if c.delay > 0 {
		time.Sleep(jitter(c.delay))
	}
}

// --- Speiseplan ------------------------------------------------------------

// rawSite is one entry of GET /speiseplan/verbrauchsorte.
type rawSite struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// rawMeal is one article of the Speiseplan API. The upstream sends 45 fields
// straight out of the kitchen's CSV export; everything not listed here is
// dropped by the JSON decoder, which is the first half of our filtering.
type rawMeal struct {
	Number       int    `json:"PRODUKTIONSNUMMER"`
	InternalName string `json:"PRODUKTIONSBEZEICHNUNG"`
	Date         string `json:"PRODUKTIONSDATUM"` // DD.MM.YYYY
	SiteNumber   int    `json:"VERBRAUCHSORTNR"`
	CategoryID   int    `json:"PRODUKTIONSARTNR"`
	Category     string `json:"PRODUKTIONSNAME"`
	Markers      string `json:"FREIKENNZEICHEN"`
	Climate      string `json:"KLIMATELLER"`
	Additives    string `json:"ZUSATZSTOFFNUMMERN"`

	PriceStudent float64 `json:"VKPREISSTUD"`
	PriceStaff   float64 `json:"VKPREISBED"`
	PriceGuest   float64 `json:"VKPREISGAST"`

	DE1 string `json:"AUSGABETEXTZEILE1"`
	DE2 string `json:"AUSGABETEXTZEILE2"`
	DE3 string `json:"AUSGABETEXTZEILE3"`
	DE4 string `json:"AUSGABETEXTZEILE4"`
	DE5 string `json:"AUSGABETEXTZEILE5"`
	DE6 string `json:"AUSGABETEXTZEILE6"`
	DE7 string `json:"AUSGABETEXTZEILE7"`

	EN1 string `json:"AUSGABETEXT2ZEILE1"`
	EN2 string `json:"AUSGABETEXT2ZEILE2"`
	EN3 string `json:"AUSGABETEXT2ZEILE3"`
	EN4 string `json:"AUSGABETEXT2ZEILE4"`
	EN5 string `json:"AUSGABETEXT2ZEILE5"`
	EN6 string `json:"AUSGABETEXT2ZEILE6"`
	EN7 string `json:"AUSGABETEXT2ZEILE7"`
	EN8 string `json:"AUSGABETEXT2ZEILE8"`
}

// germanLines returns the German description lines in order, empties skipped.
func (m rawMeal) germanLines() []string {
	return nonEmpty(m.DE1, m.DE2, m.DE3, m.DE4, m.DE5, m.DE6, m.DE7)
}

// englishLines returns the English description lines. Note the eighth line:
// the translation has one line more than the German original.
func (m rawMeal) englishLines() []string {
	return nonEmpty(m.EN1, m.EN2, m.EN3, m.EN4, m.EN5, m.EN6, m.EN7, m.EN8)
}

func (c *client) sites(ctx context.Context) ([]rawSite, error) {
	var sites []rawSite
	if err := c.get(ctx, "/speiseplan/verbrauchsorte", nil, &sites); err != nil {
		return nil, err
	}
	return sites, nil
}

// allMeals fetches the next two weeks for every location at once. The
// verbrauchsortnr filter is optional, and leaving it off returns the articles
// of all canteens mixed together — currently 1066 of them, which is four pages
// instead of the thirteen requests one call per canteen used to cost. Each
// article carries its VERBRAUCHSORTNR, so splitting them up again is free.
func (c *client) allMeals(ctx context.Context) ([]rawMeal, error) {
	var all []rawMeal
	for page := 0; page < maxPages; page++ {
		if page > 0 {
			c.pause()
		}
		q := url.Values{
			"naechste_2_wochen": {"true"},
			"limit":             {strconv.Itoa(pageSize)},
			"skip":              {strconv.Itoa(page * pageSize)},
		}
		var batch []rawMeal
		if err := c.get(ctx, "/speiseplan/artikel", q, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			return all, nil
		}
	}
	// An upstream that never returns a short page would page forever.
	return all, fmt.Errorf("artikel: more than %d pages, giving up", maxPages)
}

// --- Öffnungszeiten --------------------------------------------------------

type rawWindow struct {
	Open  string `json:"open"`
	Close string `json:"close"`
}

// rawSchedule covers all three shapes the opening-hours endpoint uses: a
// forecast day (with date and weekday), a weekday template and a weekend day.
type rawSchedule struct {
	IsOpen        bool      `json:"is_open"`
	Regular       rawWindow `json:"regular_hours"`
	Serving       rawWindow `json:"serving_hours"`
	ServingDiffer bool      `json:"serving_hours_differ"`

	Date         string `json:"date"`          // forecast only
	Weekday      string `json:"weekday"`       // forecast only
	ClosedReason string `json:"closed_reason"` // forecast only
}

type rawClosure struct {
	Label     string `json:"label"`
	Date      string `json:"date"`
	DateFrom  string `json:"date_from"`
	DateTo    string `json:"date_to"`
	IsHoliday bool   `json:"is_holiday"`
}

type rawHours struct {
	Today struct {
		Date   string `json:"date"`
		IsOpen bool   `json:"is_open"`
		Reason string `json:"reason"`
	} `json:"today"`
	Forecast []rawSchedule `json:"forecast"`
	Closures struct {
		WindowDays int          `json:"window_days"`
		Global     []rawClosure `json:"global"`
		Restaurant []rawClosure `json:"restaurant"`
	} `json:"closures"`
	Weekdays map[string]rawSchedule `json:"weekdays"`
	Weekend  map[string]rawSchedule `json:"weekend"`
}

func (c *client) hours(ctx context.Context, siteID int) (*rawHours, error) {
	q := url.Values{"restaurant_id": {strconv.Itoa(siteID)}}
	var h rawHours
	if err := c.get(ctx, "/speiseplan/restaurants/oeffnungszeiten", q, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// --- Wagtail CMS -----------------------------------------------------------

// flexInt reads a number that the CMS sometimes writes as a JSON string:
// the page detail view sends restaurant_id as 341, the list view as "341".
type flexInt int

func (f *flexInt) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("restaurant id %s: %w", data, err)
	}
	*f = flexInt(n)
	return nil
}

// rawPage is a home.MensaPage with just the hero block we care about. Address,
// map link and the human-readable name live here, not in the Speiseplan API.
type rawPage struct {
	ID   int `json:"id"`
	Meta struct {
		Slug    string `json:"slug"`
		HTMLURL string `json:"html_url"`
	} `json:"meta"`
	Title string `json:"title"`
	Intro []struct {
		Type  string `json:"type"`
		Value struct {
			Location      string `json:"location"`
			Text          string `json:"text"`
			GoogleMapsURL string `json:"google_maps_url"`
			OpeningTimes  struct {
				RestaurantID flexInt `json:"restaurant_id"`
			} `json:"opening_times"`
		} `json:"value"`
	} `json:"intro"`
}

// pages fetches all canteen pages in a single request.
func (c *client) pages(ctx context.Context) ([]rawPage, error) {
	q := url.Values{
		"type":   {"home.MensaPage"},
		"fields": {"intro"},
		"limit":  {"150"}, // the CMS refuses anything above 150
	}
	var body struct {
		Items []rawPage `json:"items"`
	}
	if err := c.get(ctx, "/pages/", q, &body); err != nil {
		return nil, err
	}
	return body.Items, nil
}
