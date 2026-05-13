package server

import (
	"net/http"
	"net/url"
	"os"
)

// Origins.LOCALHOST_IN_DEVELOPMENT is a CORS preset that mirrors BGIO's
// constant of the same name: allow localhost origins on any port, unless
// the GO_ENV environment variable is "production". Use it in the Origins
// slice on Server.
const (
	OriginLocalhostInDevelopment = "boardgame-go:localhost-in-development"
	OriginLocalhost              = "boardgame-go:localhost"
)

// applyCORS handles CORS preflight + sets the Access-Control headers on
// every response. Returns false when the request should not be forwarded
// to the router (currently only OPTIONS preflight). Origins config matches
// BGIO's behaviour: a string is matched literally, except for the two
// special sentinels above. A * entry allows all origins.
func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" && s.originAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	return true
}

func (s *Server) originAllowed(origin string) bool {
	for _, allow := range s.Origins {
		if allow == "*" {
			return true
		}
		if allow == OriginLocalhostInDevelopment {
			if os.Getenv("GO_ENV") != "production" && isLocalhost(origin) {
				return true
			}
			continue
		}
		if allow == OriginLocalhost {
			if isLocalhost(origin) {
				return true
			}
			continue
		}
		if allow == origin {
			return true
		}
	}
	return false
}

// isLocalhost recognises the common localhost forms a browser sends as
// the Origin header. The check parses the origin and compares the
// hostname exactly so attacker-controlled hostnames like
// "localhost.attacker.com" or "127.0.0.1.attacker.com" don't match —
// a prefix check would accept them.
func isLocalhost(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
