package api

import (
	"net/http"
	"strings"
)

// CORSMiddleware manages cross-origin resource sharing headers.
// It is restrictive by default (no origins allowed) unless configured via environment.
type CORSMiddleware struct {
	allowedOrigins map[string]bool
	allowAll       bool
}

// NewCORSMiddleware parses comma-delimited allowed origins (e.g. "http://localhost:3000,https://sims.corp").
func NewCORSMiddleware(allowedOriginsStr string) *CORSMiddleware {
	allowed := make(map[string]bool)
	allowAll := false

	origins := strings.Split(allowedOriginsStr, ",")
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o == "*" {
			allowAll = true
		} else if o != "" {
			allowed[o] = true
		}
	}

	return &CORSMiddleware{
		allowedOrigins: allowed,
		allowAll:       allowAll,
	}
}

// Handler intercepts HTTP requests to add CORS headers and respond to preflight OPTIONS requests.
func (c *CORSMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if c.allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if c.allowedOrigins[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
