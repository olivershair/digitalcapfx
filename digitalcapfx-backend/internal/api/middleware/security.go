package middleware

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP replaces chi's RealIP middleware, which trusts the LEFT-most
// X-Forwarded-For entry — a value the client fully controls. Each proxy in
// front of the API appends the address it received the request from, so only
// the entries added by our own proxies can be trusted: the client IP is the
// entry trustedHops positions from the right. If the header has fewer entries
// than expected (request did not come through the proxies), the TCP peer
// address is kept. trustedHops <= 0 ignores the header entirely.
func ClientIP(trustedHops int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip := clientIPFromXFF(r.Header.Values("X-Forwarded-For"), trustedHops); ip != "" {
				r.RemoteAddr = ip
			} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				r.RemoteAddr = host
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIPFromXFF(values []string, trustedHops int) string {
	if trustedHops <= 0 {
		return ""
	}
	var entries []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				entries = append(entries, p)
			}
		}
	}
	if len(entries) < trustedHops {
		return ""
	}
	ip := entries[len(entries)-trustedHops]
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

// SecurityHeaders sets defensive headers on every API response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		if !strings.HasPrefix(r.URL.Path, "/swagger/") {
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		}
		next.ServeHTTP(w, r)
	})
}
