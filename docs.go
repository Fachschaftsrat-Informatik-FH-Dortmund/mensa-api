package main

import (
	_ "embed"
	"net/http"
)

// openAPISpec is baked into the binary, so /openapi.json works no matter what
// the working directory is on the server.
//
//go:embed openapi.json
var openAPISpec []byte

// docsPage renders the spec with Swagger UI. The UI itself is loaded from a
// CDN rather than vendored: nothing of it ends up in this repository or in the
// binary, and turning the docs off (-docs=false) leaves no trace behind.
//
// Pinned to the major version: @latest would change the UI under us, an exact
// pin would leave us on a stale release.
//
// Only swagger-ui-bundle is loaded, not the standalone preset — the preset
// adds the topbar with the URL input, which is useless when the document is
// fixed.
const docsPage = `<!doctype html>
<html lang="de">
  <head>
    <title>mensa-api — API-Referenz</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <!-- Swagger UI ships no dark theme of its own (its stylesheet contains no
         prefers-color-scheme rule at all). A browser in dark mode therefore
         inverts the page algorithmically, which mangles the colours. Declaring
         the page light-only opts out of that — no colour of ours involved. -->
    <meta name="color-scheme" content="light" />
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.ui = SwaggerUIBundle({
        url: '/openapi.json',
        dom_id: '#swagger-ui',
        deepLinking: true,
        // Everything here is a GET, so the form may as well be open already.
        tryItOutEnabled: true,
        supportedSubmitMethods: ['get'],
      })
    </script>
  </body>
</html>
`

// registerDocs adds the specification and, unless switched off, the rendered
// reference. The spec stays available either way — it is useful to client
// generators even when nobody wants the HTML.
func registerDocs(mux *http.ServeMux, withUI bool) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(openAPISpec)
	})

	if !withUI {
		return
	}

	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPage))
	})
}
