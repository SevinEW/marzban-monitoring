package central

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebPanelNavigationIsolation(t *testing.T) {
	cases := []struct {
		name, method, path, site, mode, dest, host string
		allowed                                    bool
	}{
		{"external link", "GET", "/", "cross-site", "navigate", "document", "127.0.0.1:9088", true},
		{"external index link", "GET", "/index.html", "cross-site", "navigate", "document", "localhost:9088", true},
		{"external query and fragment", "GET", "/?from=chat#servers", "cross-site", "navigate", "document", "127.0.0.1:9088", true},
		{"external HEAD", "HEAD", "/", "cross-site", "navigate", "document", "[::1]:9088", true},
		{"address bar", "GET", "/", "none", "navigate", "document", "127.0.0.1:9088", true},
		{"legacy browser", "GET", "/", "", "", "", "127.0.0.1:9088", true},
		{"same-origin snapshot", "GET", "/api/snapshot", "same-origin", "cors", "empty", "127.0.0.1:9088", true},
		{"same-origin stream", "GET", "/api/stream", "same-origin", "cors", "empty", "127.0.0.1:9088", true},
		{"same-origin stylesheet", "GET", "/style.css", "same-origin", "no-cors", "style", "127.0.0.1:9088", true},
		{"external fetch", "GET", "/", "cross-site", "cors", "empty", "127.0.0.1:9088", false},
		{"external snapshot", "GET", "/api/snapshot", "cross-site", "cors", "empty", "127.0.0.1:9088", false},
		{"external stream", "GET", "/api/stream", "cross-site", "cors", "empty", "127.0.0.1:9088", false},
		{"API navigation", "GET", "/api/snapshot", "cross-site", "navigate", "document", "127.0.0.1:9088", false},
		{"stream navigation", "GET", "/api/stream", "cross-site", "navigate", "document", "127.0.0.1:9088", false},
		{"iframe", "GET", "/", "cross-site", "navigate", "iframe", "127.0.0.1:9088", false},
		{"frame", "GET", "/", "cross-site", "navigate", "frame", "127.0.0.1:9088", false},
		{"image", "GET", "/", "cross-site", "no-cors", "image", "127.0.0.1:9088", false},
		{"script", "GET", "/app.js", "cross-site", "no-cors", "script", "127.0.0.1:9088", false},
		{"POST form", "POST", "/", "cross-site", "navigate", "document", "127.0.0.1:9088", false},
		{"missing mode", "GET", "/", "cross-site", "", "document", "127.0.0.1:9088", false},
		{"missing destination", "GET", "/", "cross-site", "navigate", "", "127.0.0.1:9088", false},
		{"public host navigation", "GET", "/", "cross-site", "navigate", "document", "attacker.example", false},
		{"public host direct", "GET", "/", "none", "navigate", "document", "attacker.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := webPanelSecurity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
			r := httptest.NewRequest(tc.method, "http://127.0.0.1:9088"+tc.path, nil)
			r.Host = tc.host
			for k, v := range map[string]string{"Sec-Fetch-Site": tc.site, "Sec-Fetch-Mode": tc.mode, "Sec-Fetch-Dest": tc.dest} {
				if v != "" {
					r.Header.Set(k, v)
				}
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := http.StatusForbidden
			if tc.allowed {
				want = http.StatusNoContent
			}
			if w.Code != want || called != tc.allowed {
				t.Fatalf("status=%d called=%v want=%d allowed=%v", w.Code, called, want, tc.allowed)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("response may be cached")
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("frame isolation missing")
			}
			if !strings.Contains(w.Header().Get("Vary"), "Sec-Fetch-Mode") {
				t.Fatal("metadata variation missing")
			}
		})
	}
}
