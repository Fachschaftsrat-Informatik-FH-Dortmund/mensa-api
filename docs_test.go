package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The specification is handwritten, so nothing but a test keeps it in step
// with the routes. These two check the parts that silently rot: valid JSON,
// and one documented path per route we actually serve.

func TestOpenAPISpecIsValid(t *testing.T) {
	var spec struct {
		OpenAPI string                          `json:"openapi"`
		Info    struct{ Title, Version string } `json:"info"`
		Paths   map[string]map[string]any       `json:"paths"`
	}
	if err := json.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.1") {
		t.Errorf("openapi = %q, want a 3.1 document", spec.OpenAPI)
	}
	if spec.Info.Title == "" || spec.Info.Version == "" {
		t.Error("info.title and info.version must both be set")
	}
	if len(spec.Paths) == 0 {
		t.Fatal("the document describes no paths at all")
	}
}

func TestEveryRouteIsDocumented(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatalf("openapi.json: %v", err)
	}

	// The routes a client cares about. /docs and /openapi.json describe
	// themselves and are deliberately left out of the document.
	routes := []string{
		"/canteens",
		"/canteens/{id}",
		"/canteens/{id}/menu",
		"/canteens/{id}/menu/{date}",
		"/canteens/{id}/hours",
		"/legend",
		"/health",
	}

	for _, route := range routes {
		if _, ok := spec.Paths[route]; !ok {
			t.Errorf("route %s is served but missing from openapi.json", route)
		}
	}
	if len(spec.Paths) != len(routes) {
		t.Errorf("openapi.json describes %d paths, the server serves %d — "+
			"a documented path without a route is just as wrong",
			len(spec.Paths), len(routes))
	}
}

// The version appears twice: as a Go constant in the User-Agent we introduce
// ourselves with, and in the specification we hand out. Bumping only one of
// them is the obvious way to get this wrong.
func TestVersionsAgree(t *testing.T) {
	var spec struct {
		Info struct{ Version string } `json:"info"`
	}
	if err := json.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatalf("openapi.json: %v", err)
	}
	if spec.Info.Version != version {
		t.Errorf("openapi.json says version %q, the binary says %q",
			spec.Info.Version, version)
	}
}

func TestDocsCanBeSwitchedOff(t *testing.T) {
	for _, withDocs := range []bool{true, false} {
		handler := routes(newStore(context.Background(), newClient(0, 0), 0, 0), withDocs)

		request := httptest.NewRequest(http.MethodGet, "/docs", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		want := http.StatusOK
		if !withDocs {
			want = http.StatusNotFound
		}
		if recorder.Code != want {
			t.Errorf("GET /docs with -docs=%v: status %d, want %d", withDocs, recorder.Code, want)
		}

		// The specification stays reachable either way.
		request = httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("GET /openapi.json with -docs=%v: status %d, want 200", withDocs, recorder.Code)
		}
	}
}
