package api

import (
	"bytes"
	"compress/gzip"
	"embed"
	"html/template"
	"io"
	"net/http"
	"strings"
	"sync"
)

// The /docs page is Scalar's API reference (https://github.com/scalar/scalar,
// MIT, see scalar/LICENSE). The standalone bundle is pinned and embedded
// gzip-compressed, so serving docs needs no CDN.
const (
	// ScalarVersion is the embedded @scalar/api-reference version.
	ScalarVersion = "1.76.0"
	// ScalarAssetPath is where the embedded bundle is served. The version in the
	// path lets browsers cache it forever.
	ScalarAssetPath = "/docs/scalar-" + ScalarVersion + ".js"

	scalarCDN       = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@" + ScalarVersion + "/dist/browser/standalone.js"
	scalarIntegrity = "sha384-oH1C0RwqcwbVXkC+r8A+4qRoyEUQdtxAzOwumkxZtQBBcsy61FOoPt7B50OVJYYQ"
	scalarETag      = `"scalar-` + ScalarVersion + `"`
)

//go:embed scalar/standalone-1.76.0.js.gz scalar/LICENSE
var scalarFS embed.FS

// DocsOptions customizes the /docs page.
type DocsOptions struct {
	// Title is the browser tab title. Default "API reference".
	Title string
	// Theme is a Scalar theme name such as "default", "moon", "purple" or
	// "solarized". Empty uses Scalar's default.
	Theme string
	// DarkMode opens the page in dark mode.
	DarkMode bool
	// CDN loads the same pinned bundle from jsDelivr, checked with subresource
	// integrity, instead of serving the embedded copy.
	CDN bool
}

var docsPage = template.Must(template.New("docs").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
</head>
<body>
<div id="app"></div>
<script src="{{.Script}}"{{if .Integrity}} integrity="{{.Integrity}}" crossorigin="anonymous"{{end}}></script>
<script>Scalar.createApiReference('#app', {{.Config}})</script>
</body>
</html>
`))

// DocsPage returns the handler for the API reference page, which loads the
// OpenAPI document from specURL.
func DocsPage(specURL string, opts DocsOptions) http.HandlerFunc {
	config := map[string]any{"url": specURL, "darkMode": opts.DarkMode}
	if opts.Theme != "" {
		config["theme"] = opts.Theme
	}
	data := struct {
		Title, Script, Integrity string
		Config                   map[string]any
	}{Title: opts.Title, Script: ScalarAssetPath, Config: config}
	if data.Title == "" {
		data.Title = "API reference"
	}
	if opts.CDN {
		data.Script, data.Integrity = scalarCDN, scalarIntegrity
	}

	var page bytes.Buffer
	if err := docsPage.Execute(&page, data); err != nil {
		panic("api: rendering docs page: " + err.Error()) // the template is static; only a programming error fails
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(page.Bytes())
	}
}

var (
	scalarGzip = mustRead("scalar/standalone-" + ScalarVersion + ".js.gz")
	// scalarPlain is decompressed on first use, only for clients without gzip.
	scalarPlain = sync.OnceValue(func() []byte {
		r, err := gzip.NewReader(bytes.NewReader(scalarGzip))
		if err != nil {
			panic("api: embedded Scalar bundle is not gzip: " + err.Error())
		}
		b, err := io.ReadAll(r)
		if err != nil {
			panic("api: embedded Scalar bundle is corrupt: " + err.Error())
		}
		return b
	})
)

func mustRead(name string) []byte {
	b, err := scalarFS.ReadFile(name)
	if err != nil {
		panic("api: missing embedded file " + name)
	}
	return b
}

// ScalarAsset serves the embedded Scalar bundle: gzip-compressed when the client
// accepts it, cached for a year (the URL changes with the version).
func ScalarAsset(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("ETag", scalarETag)
	h.Set("Vary", "Accept-Encoding")
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == scalarETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := scalarGzip
	if acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
	} else {
		body = scalarPlain()
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

func acceptsGzip(r *http.Request) bool {
	for part := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") && strings.ReplaceAll(params, " ", "") != "q=0" {
			return true
		}
	}
	return false
}
