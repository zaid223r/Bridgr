package api

import (
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getPage(t *testing.T, opts DocsOptions) string {
	t.Helper()
	rec := httptest.NewRecorder()
	DocsPage("/openapi.json", opts)(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("status %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	return rec.Body.String()
}

func TestDocsPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		opts     DocsOptions
		contains []string
		absent   []string
	}{
		{
			name: "defaults",
			opts: DocsOptions{},
			contains: []string{
				"<title>API reference</title>",
				`<script src="` + ScalarAssetPath + `"></script>`,
				`Scalar.createApiReference('#app', {"darkMode":false,"url":"/openapi.json"})`,
			},
			absent: []string{"integrity", "cdn.jsdelivr.net", "theme"},
		},
		{
			name:     "theme, dark mode and title",
			opts:     DocsOptions{Title: "Shop API", Theme: "moon", DarkMode: true},
			contains: []string{"<title>Shop API</title>", `{"darkMode":true,"theme":"moon","url":"/openapi.json"}`},
		},
		{
			name: "CDN with subresource integrity",
			opts: DocsOptions{CDN: true},
			contains: []string{
				`src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@` + ScalarVersion + `/dist/browser/standalone.js"`,
				`integrity="` + scalarIntegrity + `" crossorigin="anonymous"`,
			},
			absent: []string{ScalarAssetPath},
		},
		{
			name: "hostile title and theme are escaped",
			opts: DocsOptions{Title: "</title><script>alert(1)</script>", Theme: "x'});alert(1);//</script><script>alert(2)"},
			contains: []string{
				"&lt;/title&gt;&lt;script&gt;alert(1)&lt;/script&gt;",
				// The theme stays inside a JSON string and can't close the script tag.
				`"theme":"x'});alert(1);//\u003c/script\u003e\u003cscript\u003ealert(2)"`,
			},
			absent: []string{"<script>alert(1)</script>", "<script>alert(2)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Attribute values may be HTML-escaped (+ becomes &#43;), so "contains"
			// accepts either form; "absent" checks the raw bytes a browser parses.
			page := getPage(t, tt.opts)
			decoded := html.UnescapeString(page)
			for _, want := range tt.contains {
				if !strings.Contains(page, want) && !strings.Contains(decoded, want) {
					t.Errorf("page does not contain %s\n%s", want, page)
				}
			}
			for _, bad := range tt.absent {
				if strings.Contains(page, bad) {
					t.Errorf("page contains %s", bad)
				}
			}
		})
	}
}

func getAsset(t *testing.T, method string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, ScalarAssetPath, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	ScalarAsset(rec, req)
	return rec
}

func sri(b []byte) string {
	sum := sha512.Sum384(b)
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

func TestScalarAsset(t *testing.T) {
	t.Parallel()

	gz := getAsset(t, "GET", map[string]string{"Accept-Encoding": "br, gzip"})
	if gz.Code != http.StatusOK || gz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip request: status %d, encoding %q", gz.Code, gz.Header().Get("Content-Encoding"))
	}
	r, err := gzip.NewReader(gz.Body)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	// The embedded copy must be byte-identical to the CDN file the integrity
	// hash pins, or CDN mode and embedded mode would serve different code.
	if got := sri(unzipped); got != scalarIntegrity {
		t.Errorf("embedded bundle hash %s, want %s", got, scalarIntegrity)
	}

	for _, enc := range []string{"", "br", "gzip;q=0"} {
		plain := getAsset(t, "GET", map[string]string{"Accept-Encoding": enc})
		if plain.Header().Get("Content-Encoding") != "" || !bytes.Equal(plain.Body.Bytes(), unzipped) {
			t.Errorf("Accept-Encoding %q: want the uncompressed bundle", enc)
		}
	}

	h := gz.Header()
	for k, want := range map[string]string{
		"Content-Type":  "text/javascript; charset=utf-8",
		"Cache-Control": "public, max-age=31536000, immutable",
		"ETag":          scalarETag,
		"Vary":          "Accept-Encoding",
	} {
		if h.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, h.Get(k), want)
		}
	}

	if notMod := getAsset(t, "GET", map[string]string{"If-None-Match": scalarETag}); notMod.Code != http.StatusNotModified || notMod.Body.Len() != 0 {
		t.Errorf("If-None-Match: status %d, %d body bytes", notMod.Code, notMod.Body.Len())
	}
	if head := getAsset(t, "HEAD", nil); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD: status %d, %d body bytes", head.Code, head.Body.Len())
	}
}

func TestScalarLicenseIsEmbedded(t *testing.T) {
	t.Parallel()
	if !strings.Contains(string(mustRead("scalar/LICENSE")), "MIT License") {
		t.Error("Scalar's MIT license must ship with the embedded bundle")
	}
}
