package mock

import (
	"net/http"
	"strconv"
	"strings"
)

// Options configures the mock. Everything here is about CORS, because the mock
// runs on its own port and is therefore always a cross-origin call from a
// browser: without these headers a frontend cannot reach it at all.
type Options struct {
	// AllowOrigins are the origins permitted to call the mock. Empty means any
	// origin, sent as "*".
	//
	// "*" is the right default for a mock — it exists to be called from
	// wherever the frontend happens to be running — but it is incompatible with
	// credentials, which is why AllowCredentials changes how this is answered
	// rather than being an independent switch.
	AllowOrigins []string

	// AllowCredentials permits the browser to send cookies and Authorization
	// headers.
	//
	// The CORS specification forbids combining credentials with a wildcard
	// origin: a browser rejects "*" outright when credentials are requested. So
	// with this on, the caller's own Origin is echoed back instead — which is
	// only safe because the mock serves fabricated data and holds nothing worth
	// stealing. The same trick on a real API would be a vulnerability.
	AllowCredentials bool

	// AllowMethods and AllowHeaders default to everything, since the mock
	// stands in for an API whose shape the caller already knows.
	AllowMethods []string
	AllowHeaders []string

	// MaxAge is how long a browser may cache the preflight, in seconds.
	// Zero leaves it unset and the browser uses its own default.
	MaxAge int

	// EnforceAuth makes the mock check documented security requirements: a
	// request to a protected operation without credentials gets a 401 problem
	// document naming the scheme it lacks. Presence is checked, not validity —
	// the mock cannot know what a real token looks like, but it can catch the
	// frontend that forgot to send one at all.
	EnforceAuth bool
}

// DefaultOptions is what the mock uses when nothing is configured: open to any
// origin, no credentials.
func DefaultOptions() Options {
	return Options{}
}

func (o Options) methods() string {
	if len(o.AllowMethods) == 0 {
		return "GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS"
	}
	return strings.Join(o.AllowMethods, ",")
}

func (o Options) headers() string {
	if len(o.AllowHeaders) == 0 {
		return "*"
	}
	return strings.Join(o.AllowHeaders, ",")
}

// originFor decides what to answer in Access-Control-Allow-Origin, and reports
// whether the request is allowed at all.
//
// A request with no Origin header is not a CORS request — curl, a server-side
// client, a test — and is always allowed; the header is simply irrelevant to it.
func (o Options) originFor(requestOrigin string) (value string, allowed bool) {
	if len(o.AllowOrigins) == 0 || contains(o.AllowOrigins, "*") {
		if o.AllowCredentials {
			// A wildcard is rejected by browsers alongside credentials, so the
			// caller's origin is echoed. Safe here, and only here: the mock has
			// no real data and no real session behind it.
			if requestOrigin == "" {
				return "", true
			}
			return requestOrigin, true
		}
		return "*", true
	}

	if requestOrigin == "" {
		return "", true // not a browser request; nothing to match against
	}
	if contains(o.AllowOrigins, requestOrigin) {
		return requestOrigin, true
	}
	return "", false
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

// applyCORS writes the response headers and reports whether the request may
// proceed. A disallowed origin gets no CORS headers at all, which is what makes
// the browser block it — the request itself is still answered, because refusing
// it server-side would misrepresent how CORS works.
func (o Options) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin, allowed := o.originFor(r.Header.Get("Origin"))
	if !allowed {
		return false
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	if o.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	// Whenever the answer depends on the request's Origin, caches have to be
	// told, or one origin's response is served to another.
	if origin != "*" && origin != "" {
		w.Header().Add("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", o.methods())
	w.Header().Set("Access-Control-Allow-Headers", o.headers())
	if o.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", strconv.Itoa(o.MaxAge))
	}
	return true
}
