package main

import (
	"context"
	"log"
	"slices"
	"strings"
	"sync"
	"time"
)

// store holds everything we serve. Requests never reach stwdo.de: a background
// goroutine refreshes the whole dataset on a timer, and handlers only ever
// read from memory. That keeps the load on the upstream constant (one sweep
// per interval) no matter how many clients we have.
type store struct {
	client   *client
	cmsEvery time.Duration

	mu         sync.RWMutex
	canteens   []Canteen
	menus      map[int][]Day
	hours      map[int]Hours
	pages      []rawPage
	updated    time.Time
	cmsUpdated time.Time
	lastErr    string
}

func newStore(c *client, cmsEvery time.Duration) *store {
	return &store{
		client:   c,
		cmsEvery: cmsEvery,
		menus:    map[int][]Day{},
		hours:    map[int]Hours{},
	}
}

// refresh pulls a complete new dataset. On a partial failure the previous
// values stay in place — serving slightly stale data beats serving none.
func (s *store) refresh(ctx context.Context) {
	start := time.Now()

	sites, err := s.client.sites(ctx)
	if err != nil {
		s.noteError("verbrauchsorte: " + err.Error())
		log.Printf("refresh: cannot list canteens: %v", err)
		return
	}

	pages := s.cachedPages()
	if len(pages) == 0 || time.Since(s.cmsAge()) > s.cmsEvery {
		fresh, err := s.client.pages(ctx)
		if err != nil {
			// Not fatal: without the CMS we lose addresses and nice names,
			// but the menu itself is unaffected.
			log.Printf("refresh: cannot read CMS pages, keeping previous: %v", err)
		} else {
			pages = fresh
			s.mu.Lock()
			s.pages, s.cmsUpdated = fresh, time.Now()
			s.mu.Unlock()
		}
	}

	canteens := mergeCanteens(sites, pages)
	menus := map[int][]Day{}
	hours := map[int]Hours{}
	var failures []string

	for i, canteen := range canteens {
		if days, err := s.client.mealsForSite(ctx, canteen.ID); err != nil {
			log.Printf("refresh: menu for %d (%s): %v", canteen.ID, canteen.Name, err)
			failures = append(failures, canteen.Name+" (Speiseplan)")
			if previous, ok := s.menuFor(canteen.ID); ok {
				menus[canteen.ID] = previous
			}
		} else {
			menus[canteen.ID] = groupDays(days)
		}
		canteens[i].HasMenu = len(menus[canteen.ID]) > 0

		if raw, err := s.client.hours(ctx, canteen.ID); err != nil {
			log.Printf("refresh: hours for %d (%s): %v", canteen.ID, canteen.Name, err)
			failures = append(failures, canteen.Name+" (Öffnungszeiten)")
			if previous, ok := s.hoursFor(canteen.ID); ok {
				hours[canteen.ID] = previous
			}
		} else {
			hours[canteen.ID] = toHours(canteen.ref(), raw)
		}

		if ctx.Err() != nil {
			return
		}
	}

	s.mu.Lock()
	s.canteens, s.menus, s.hours = canteens, menus, hours
	s.updated = time.Now()
	s.lastErr = strings.Join(failures, ", ")
	s.mu.Unlock()

	meals := 0
	for _, days := range menus {
		for _, day := range days {
			for _, category := range day.Categories {
				meals += len(category.Meals)
			}
		}
	}
	log.Printf("refresh: %d canteens, %d meals, %d failures, took %s",
		len(canteens), meals, len(failures), time.Since(start).Round(time.Millisecond))
}

// run refreshes once and then on every tick until the context is cancelled.
func (s *store) run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh(ctx)
		}
	}
}

// mergeCanteens combines both sources of truth. /verbrauchsorte knows which
// locations have menu data but spells their names for the kitchen ("Hauptmensa
// inklKalteKüche"); the CMS knows the public name, the address and — crucially
// — three locations the Speiseplan API forgets entirely (453, 467, 468). One
// CMS page can carry several hero blocks, which is how Café C ends up as two.
func mergeCanteens(sites []rawSite, pages []rawPage) []Canteen {
	byID := map[int]*Canteen{}

	for _, site := range sites {
		byID[site.ID] = &Canteen{ID: site.ID, Name: site.Name}
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
	return slices.Clone(s.canteens), s.updated
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

func (s *store) cmsAge() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cmsUpdated
}

func (s *store) noteError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErr = msg
}

// status is what GET /health reports.
type status struct {
	Status      string    `json:"status"`
	Canteens    int       `json:"canteens"`
	Meals       int       `json:"meals"`
	Updated     time.Time `json:"updated"`
	AgeSeconds  int       `json:"ageSeconds"`
	LastFailure string    `json:"lastFailure,omitempty"`
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

	state := "ok"
	if s.updated.IsZero() {
		state = "warming up"
	} else if s.lastErr != "" {
		state = "degraded"
	}

	return status{
		Status:      state,
		Canteens:    len(s.canteens),
		Meals:       meals,
		Updated:     s.updated,
		AgeSeconds:  int(time.Since(s.updated).Seconds()),
		LastFailure: s.lastErr,
	}
}
