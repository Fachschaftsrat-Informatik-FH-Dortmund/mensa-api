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
	refresh := flag.Duration("refresh", time.Hour, "how often to refresh menus and opening hours")
	cmsRefresh := flag.Duration("cms-refresh", 24*time.Hour, "how often to refresh addresses and names from the CMS")
	timeout := flag.Duration("timeout", 20*time.Second, "timeout for a single upstream request")
	delay := flag.Duration("delay", 200*time.Millisecond, "pause between upstream requests")
	docs := flag.Bool("docs", true, "serve the rendered API reference at /docs")
	flag.Parse()

	st := newStore(newClient(*timeout, *delay), *cmsRefresh)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Fill the cache before opening the port: a server that answers with
	// "warming up" is more confusing than one that starts a few seconds later.
	log.Printf("filling cache from %s ...", upstreamBase)
	st.refresh(ctx)
	go st.run(ctx, *refresh)

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

	log.Printf("listening on %s, refreshing every %s", *addr, *refresh)
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
		hours, found := st.hoursFor(canteen.ID)
		if !found {
			writeError(w, http.StatusNotFound, "no opening hours cached for this canteen")
			return
		}
		writeJSON(w, http.StatusOK, hours)
	})

	return mux
}

// lookup resolves the {id} path value and writes the error response itself if
// it does not name a canteen we know.
func lookup(st *store, w http.ResponseWriter, r *http.Request) (Canteen, bool) {
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
