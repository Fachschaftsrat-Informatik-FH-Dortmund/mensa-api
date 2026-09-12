package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// store holds everything we serve. Requests never reach stwdo.de directly:
// handlers only ever read from memory, and the memory is filled on demand.
//
// There is no background ticker. A cache group is refetched the first time a
// request needs it and its data has gone stale, which means a service nobody
// asks anything makes no upstream requests at all — and the ones it does make
// follow real traffic instead of a clock we would otherwise leave as a
// signature in someone else's access log.
type store struct {
	base   context.Context // outlives a single request; background refreshes use it
	client *client
	ttl    time.Duration // menus and opening hours
	cmsTTL time.Duration // the canteen list

	catalogue slot
	menuSlot  slot

	slotsMu   sync.Mutex
	hourSlots map[int]*slot

	mu          sync.RWMutex
	canteens    []Canteen
	pages       []rawPage
	menus       map[int][]Day
	hours       map[int]Hours
	catalogueAt time.Time
	menusAt     time.Time
	hoursAt     map[int]time.Time
	failures    map[string]string // cache group -> last failure, cleared on success
}

func newStore(base context.Context, c *client, ttl, cmsTTL time.Duration) *store {
	return &store{
		base:      base,
		client:    c,
		ttl:       ttl,
		cmsTTL:    cmsTTL,
		hourSlots: map[int]*slot{},
		menus:     map[int][]Day{},
		hours:     map[int]Hours{},
		hoursAt:   map[int]time.Time{},
		failures:  map[string]string{},
	}
}

// --- freshness -------------------------------------------------------------

// retryAfter is how long a cache group stays quiet after a failed refresh.
// Without it a broken upstream would be hit again by every single request.
const retryAfter = 90 * time.Second

// slot tracks the freshness of one cache group and makes sure at most one
// refresh for it is in flight.
type slot struct {
	mu       sync.Mutex
	expires  time.Time
	loaded   bool
	inFlight bool
}

// ensure brings the slot up to date around fn, which does the fetching and
// stores whatever it got. Three cases:
//
//   - fresh: fn is not called.
//   - stale, but something is cached: fn runs in the background and the caller
//     returns at once with the slightly older data. Nobody waits for the
//     network on a warm cache.
//   - nothing cached yet: the caller waits. Further callers queue on the mutex
//     and find the slot fresh when they get it, so a cold start costs one
//     fetch no matter how many requests arrive together.
func (s *slot) ensure(ctx context.Context, ttl time.Duration, fn func(context.Context) error) {
	s.mu.Lock()
	if time.Now().Before(s.expires) {
		s.mu.Unlock()
		return
	}

	if !s.loaded {
		defer s.mu.Unlock()
		s.finish(fn(ctx), ttl)
		return
	}

	if s.inFlight {
		s.mu.Unlock()
		return
	}
	s.inFlight = true
	s.mu.Unlock()

	go func() {
		err := fn(ctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.inFlight = false
		s.finish(err, ttl)
	}()
}

// finish records the outcome of a refresh; the mutex must be held. A failure
// buys a much shorter quiet period than a success, so a passing outage heals
// on its own without anyone hammering stwdo.de in the meantime.
func (s *slot) finish(err error, ttl time.Duration) {
	if err != nil {
		s.expires = time.Now().Add(jitter(retryAfter))
		return
	}
	s.loaded = true
	s.expires = time.Now().Add(jitter(ttl))
}

// jitter spreads a duration over ±20 % of its length. A service that is polled
// regularly would otherwise settle into a fixed rhythm and call the upstream
// at the same second of every hour; there is no reason to be that recognisable.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	spread := int64(d / 5)
	return d - time.Duration(spread) + time.Duration(rand.Int64N(2*spread+1))
}

// --- the three cache groups ------------------------------------------------

// ensureCanteens guarantees that the canteen list is good enough to answer
// with. Everything else needs it too, if only to tell a known id from a typo.
func (s *store) ensureCanteens() {
	s.catalogue.ensure(s.base, s.cmsTTL, s.fetchCanteens)
}

func (s *store) ensureMenus() {
	s.menuSlot.ensure(s.base, s.ttl, s.fetchMenus)
}

// ensureHours refreshes one canteen's opening times. This is the only group
// kept per canteen: the upstream insists on a restaurant_id, so there is no
// way to ask for all of them at once — and no reason to fetch the hours of a
// canteen nobody has asked about.
func (s *store) ensureHours(id int) {
	s.slotsMu.Lock()
	sl, ok := s.hourSlots[id]
	if !ok {
		sl = &slot{}
		s.hourSlots[id] = sl
	}
	s.slotsMu.Unlock()

	sl.ensure(s.base, s.ttl, func(ctx context.Context) error {
		return s.fetchHours(ctx, id)
	})
}

// fetchCanteens reloads which canteens exist, from both sources of truth: two
// requests, and the list changes about once a semester.
func (s *store) fetchCanteens(ctx context.Context) error {
	sites, err := s.client.sites(ctx)
	if err != nil {
		s.noteFailure("canteens", "Mensa-Liste: "+err.Error())
		log.Printf("canteens: cannot list canteens: %v", err)
		return err
	}

	s.client.pause()
	pages, err := s.client.pages(ctx)
	if err != nil {
		// Not fatal: without the CMS we lose addresses and the public names,
		// but the menu itself is unaffected. Keep whatever we had.
		log.Printf("canteens: cannot read CMS pages, keeping previous: %v", err)
		pages = s.cachedPages()
	}

	canteens := mergeCanteens(sites, pages)

	s.mu.Lock()
	s.canteens, s.catalogueAt = canteens, time.Now()
	if len(pages) > 0 {
		s.pages = pages
	}
	s.mu.Unlock()

	s.clearFailure("canteens")
	log.Printf("canteens: %d locations", len(canteens))
	return nil
}

// fetchMenus pulls the next two weeks for every canteen in one go and splits
// the result up afterwards. See client.allMeals for why that is one request
// per 300 meals rather than one per canteen.
func (s *store) fetchMenus(ctx context.Context) error {
	meals, err := s.client.allMeals(ctx)
	if err != nil {
		s.noteFailure("menus", "Speisepläne: "+err.Error())
		log.Printf("menus: %v", err)
		return err
	}

	bySite := map[int][]rawMeal{}
	for _, meal := range meals {
		bySite[meal.SiteNumber] = append(bySite[meal.SiteNumber], meal)
	}
	menus := make(map[int][]Day, len(bySite))
	for id, raw := range bySite {
		menus[id] = groupDays(raw)
	}

	s.mu.Lock()
	s.menus, s.menusAt = menus, time.Now()
	s.mu.Unlock()

	s.clearFailure("menus")
	log.Printf("menus: %d meals for %d canteens", len(meals), len(menus))
	return nil
}

func (s *store) fetchHours(ctx context.Context, id int) error {
	canteen, found := s.canteen(id)
	if !found {
		return fmt.Errorf("unknown canteen %d", id)
	}

	raw, err := s.client.hours(ctx, id)
	if err != nil {
		s.noteFailure(hoursKey(id), canteen.Name+" (Öffnungszeiten): "+err.Error())
		log.Printf("hours for %d (%s): %v", id, canteen.Name, err)
		return err
	}

	s.mu.Lock()
	s.hours[id] = toHours(canteen.ref(), raw)
	s.hoursAt[id] = time.Now()
	s.mu.Unlock()

	s.clearFailure(hoursKey(id))
	return nil
}

func hoursKey(id int) string { return "hours/" + fmt.Sprint(id) }

// warm fills the whole cache up front. Off by default — the point of the lazy
// design is that an idle deployment is silent — but a busy instance may prefer
// to pay the first fetch at startup instead of making a guest wait for it.
func (s *store) warm() {
	s.ensureCanteens()
	s.ensureMenus()
}

// mergeCanteens combines both sources of truth. /verbrauchsorte knows which
// locations have menu data but spells their names for the kitchen ("Hauptmensa
// inklKalteKüche"); the CMS knows the public name, the address and — crucially
// — three locations the Speiseplan API forgets entirely (453, 467, 468). One
// CMS page can carry several hero blocks, which is how Café C ends up as two.
func mergeCanteens(sites []rawSite, pages []rawPage) []Canteen {
	byID := map[int]*Canteen{}

	// Appearing in /verbrauchsorte is what hasMenu means: this location is
	// known to the Speiseplan API, so asking for its menu is worth the try.
	// Whether it currently has meals on offer is a different question — and
	// one we would have to fetch every menu to answer.
	for _, site := range sites {
		byID[site.ID] = &Canteen{ID: site.ID, Name: site.Name, HasMenu: true}
	}

	for _, page := range pages {
		for _, block := range page.Intro {
			id := int(block.Value.OpeningTimes.RestaurantID)
			if block.Type != "mensa_hero" || id == 0 {
				continue
			}
			canteen, ok := byID[id]
			if !ok {
				canteen = &Canteen{ID: id}
				byID[id] = canteen
			}
			canteen.Name = page.Title // the name guests actually see
			canteen.Slug = page.Meta.Slug
			canteen.URL = page.Meta.HTMLURL
			canteen.Address = block.Value.Location
			canteen.Description = block.Value.Text
			canteen.MapsURL = block.Value.GoogleMapsURL
		}
	}

	out := make([]Canteen, 0, len(byID))
	for _, canteen := range byID {
		out = append(out, *canteen)
	}
	slices.SortFunc(out, func(a, b Canteen) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// --- reads -----------------------------------------------------------------

func (s *store) allCanteens() ([]Canteen, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.canteens), s.catalogueAt
}

func (s *store) canteen(id int) (Canteen, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, canteen := range s.canteens {
		if canteen.ID == id {
			return canteen, true
		}
	}
	return Canteen{}, false
}

func (s *store) menuFor(id int) ([]Day, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	days, ok := s.menus[id]
	return days, ok
}

func (s *store) hoursFor(id int) (Hours, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hours[id]
	return h, ok
}

func (s *store) cachedPages() []rawPage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pages
}

func (s *store) noteFailure(key, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[key] = msg
}

func (s *store) clearFailure(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, key)
}

// --- health ----------------------------------------------------------------

// status is what GET /health reports. Reading it never triggers a fetch: the
// endpoint is polled by the deploy workflow and by monitoring, and a monitor
// that kept the cache warm would defeat the whole point of loading on demand.
type status struct {
	Status      string      `json:"status"`
	Canteens    int         `json:"canteens"`
	Meals       int         `json:"meals"`
	Cache       cacheStatus `json:"cache"`
	LastFailure string      `json:"lastFailure,omitempty"`
}

// cacheStatus reports every group on its own — with nothing running on a
// timer there is no single "last run" left to report.
type cacheStatus struct {
	Canteens *groupStatus `json:"canteens"` // null: not loaded yet
	Menus    *groupStatus `json:"menus"`
	Hours    hoursCache   `json:"hours"`
}

type groupStatus struct {
	Updated    time.Time `json:"updated"`
	AgeSeconds int       `json:"ageSeconds"`
}

// hoursCache summarises the per-canteen group: how many are cached, and how
// stale the worst of them is.
type hoursCache struct {
	Canteens         int `json:"canteens"`
	OldestAgeSeconds int `json:"oldestAgeSeconds"`
}

func age(t time.Time) *groupStatus {
	if t.IsZero() {
		return nil
	}
	return &groupStatus{Updated: t, AgeSeconds: int(time.Since(t).Seconds())}
}

func (s *store) status() status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meals := 0
	for _, days := range s.menus {
		for _, day := range days {
			for _, category := range day.Categories {
				meals += len(category.Meals)
			}
		}
	}

	oldest := 0
	for _, t := range s.hoursAt {
		if seconds := int(time.Since(t).Seconds()); seconds > oldest {
			oldest = seconds
		}
	}

	messages := make([]string, 0, len(s.failures))
	for _, msg := range s.failures {
		messages = append(messages, msg)
	}
	sort.Strings(messages) // map order would make /health flap between polls

	// "cold" is not a complaint: a deployment nobody has queried yet has an
	// empty cache by design, and fills it with the first request.
	state := "ok"
	switch {
	case len(messages) > 0:
		state = "degraded"
	case s.catalogueAt.IsZero():
		state = "cold"
	}

	return status{
		Status:   state,
		Canteens: len(s.canteens),
		Meals:    meals,
		Cache: cacheStatus{
			Canteens: age(s.catalogueAt),
			Menus:    age(s.menusAt),
			Hours:    hoursCache{Canteens: len(s.hoursAt), OldestAgeSeconds: oldest},
		},
		LastFailure: strings.Join(messages, ", "),
	}
}
