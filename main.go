package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	ttl := flag.Duration("ttl", time.Hour, "how long cached menus and opening hours stay fresh")
	canteensTTL := flag.Duration("canteens-ttl", 24*time.Hour, "how long the cached canteen list stays fresh")
	timeout := flag.Duration("timeout", 20*time.Second, "timeout for a single upstream request")
	delay := flag.Duration("delay", 200*time.Millisecond, "pause between consecutive upstream requests")
	warm := flag.Bool("warm", false, "fetch canteens and menus at startup instead of on first use")
	docs := flag.Bool("docs", true, "serve the rendered API reference at /docs")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st := newStore(ctx, newClient(*timeout, *delay), *ttl, *canteensTTL)

	// Nothing is fetched until a request needs it, so the port can open at
	// once. -warm trades that silence for a faster first response.
	if *warm {
		log.Printf("filling cache from %s ...", upstreamBase)
		st.warm()
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(routes(st, *docs)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	log.Printf("listening on %s, cached data stays fresh for %s (canteen list %s)",
		*addr, *ttl, *canteensTTL)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	log.Print("stopped")
}

func routes(st *store, withDocs bool) http.Handler {
	mux := http.NewServeMux()
	registerDocs(mux, withDocs)

	endpoints := []string{
		"GET /canteens",
		"GET /canteens/{id}",
		"GET /canteens/{id}/menu",
		"GET /canteens/{id}/menu/{date}",
		"GET /canteens/{id}/hours",
		"GET /legend",
		"GET /health",
		"GET /openapi.json",
	}
	if withDocs {
		endpoints = append(endpoints, "GET /docs")
	}

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service":   "mensa-api",
			"source":    upstreamBase + "/speiseplan",
			"endpoints": endpoints,
		})
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, st.status())
	})

	mux.HandleFunc("GET /legend", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, buildLegend())
	})

	mux.HandleFunc("GET /canteens", func(w http.ResponseWriter, r *http.Request) {
		st.ensureCanteens()
		canteens, updated := st.allCanteens()
		writeJSON(w, http.StatusOK, map[string]any{
			"updated":  updated,
			"canteens": canteens,
		})
	})

	mux.HandleFunc("GET /canteens/{id}", func(w http.ResponseWriter, r *http.Request) {
		canteen, ok := lookup(st, w, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, canteen)
	})

	mux.HandleFunc("GET /canteens/{id}/menu", func(w http.ResponseWriter, r *http.Request) {
		canteen, ok := lookup(st, w, r)
		if !ok {
			return
		}
		st.ensureMenus()
		days, _ := st.menuFor(canteen.ID)
		if days == nil {
			days = []Day{}
		}
		writeJSON(w, http.StatusOK, MenuResponse{Canteen: canteen.ref(), Days: days})
	})

	mux.HandleFunc("GET /canteens/{id}/menu/{date}", func(w http.ResponseWriter, r *http.Request) {
		canteen, ok := lookup(st, w, r)
		if !ok {
			return
		}
		date := r.PathValue("date")
		if date == "today" {
			date = time.Now().Format("2006-01-02")
		}
		if !validDate(date) {
			writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD or 'today'")
			return
		}

		st.ensureMenus()
		days, _ := st.menuFor(canteen.ID)
		response := DayResponse{Canteen: canteen.ref(), Date: date, Categories: []Category{}}
		for _, day := range days {
			if day.Date == date {
				response.Categories = day.Categories
				break
			}
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("GET /canteens/{id}/hours", func(w http.ResponseWriter, r *http.Request) {
		canteen, ok := lookup(st, w, r)
		if !ok {
			return
		}
		st.ensureHours(canteen.ID)
		hours, found := st.hoursFor(canteen.ID)
		if !found {
			writeError(w, http.StatusNotFound, "no opening hours available for this canteen")
			return
		}
		writeJSON(w, http.StatusOK, hours)
	})

	return mux
}

// lookup resolves the {id} path value and writes the error response itself if
// it does not name a canteen we know. It is also where every /canteens/{id}
// route pulls in the canteen list, which it needs to tell a valid id from a
// typo in the first place.
func lookup(st *store, w http.ResponseWriter, r *http.Request) (Canteen, bool) {
	st.ensureCanteens()

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "canteen id must be a number")
		return Canteen{}, false
	}
	canteen, found := st.canteen(id)
	if !found {
		writeError(w, http.StatusNotFound, "unknown canteen, see GET /canteens")
		return Canteen{}, false
	}
	return canteen, true
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// The upstream sends no CORS headers at all, which is the main reason a
	// browser cannot talk to it directly. Ours may.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(code)

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// logRequests writes one line per request: method, path, status, duration.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(recorder, r)

		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path += "?" + r.URL.RawQuery
		}
		log.Printf("%s %s %d %s", r.Method, path, recorder.code,
			time.Since(start).Round(time.Microsecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}
