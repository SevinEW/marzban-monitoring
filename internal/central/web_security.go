package central

import (
	"net"
	"net/http"
)

// Only the read-only landing document may be opened from an external link.
// Subresource loads, embedded frames, and telemetry APIs remain isolated.
func webExternalNavigation(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		(r.URL.Path == "/" || r.URL.Path == "/index.html") &&
		r.Header.Get("Sec-Fetch-Mode") == "navigate" &&
		r.Header.Get("Sec-Fetch-Dest") == "document"
}

func webPanelSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Apply these to errors too, so a rejected navigation cannot be cached.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Add("Vary", "Sec-Fetch-Site, Sec-Fetch-Mode, Sec-Fetch-Dest")
		// The listener remains private behind the authenticated TLS reverse proxy.
		hostname, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			hostname = r.Host
		}
		ip := net.ParseIP(hostname)
		if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "Invalid host", http.StatusForbidden)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" && !webExternalNavigation(r) {
			http.Error(w, "Cross-site request denied", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
