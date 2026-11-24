package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPFromXFF(t *testing.T) {
	cases := []struct {
		name string
		xff  []string
		hops int
		want string
	}{
		{"spoofed left entry ignored (1 hop)", []string{"6.6.6.6, 41.202.1.9"}, 1, "41.202.1.9"},
		{"next proxy + cloud run (2 hops)", []string{"6.6.6.6, 41.202.1.9, 34.1.2.3"}, 2, "41.202.1.9"},
		{"multiple header lines", []string{"6.6.6.6", "41.202.1.9, 34.1.2.3"}, 2, "41.202.1.9"},
		{"too few entries → fall back", []string{"41.202.1.9"}, 2, ""},
		{"not an IP", []string{"evil, 34.1.2.3"}, 2, ""},
		{"hops 0 ignores header", []string{"1.2.3.4"}, 0, ""},
	}
	for _, c := range cases {
		if got := clientIPFromXFF(c.xff, c.hops); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestClientIP_FallsBackToPeer(t *testing.T) {
	var seen string
	h := ClientIP(2)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.RemoteAddr }))
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.7:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4") // single client-supplied entry
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "10.0.0.7" {
		t.Errorf("RemoteAddr = %q, want peer 10.0.0.7", seen)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/profile", nil))
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Strict-Transport-Security", "Content-Security-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
}
