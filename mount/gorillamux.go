package mount

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/user/specter"
)

// GorillaMux registers the console on a gorilla/mux router under cfg.BasePath
// (default "/docs").
//
// mux matches a PathPrefix against the whole subtree, so like chi this is one
// registration rather than one per endpoint: anything the console adds later is
// served without touching this function.
func GorillaMux(r *mux.Router, cfg specter.Config) {
	base, h := prepare(cfg)

	// The bare mount point is registered before the prefix so it is matched
	// first: mux tries routes in the order they were added, and the prefix
	// would otherwise swallow the redirect.
	r.HandleFunc(base, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, redirectTarget(base, req.URL.RawQuery), http.StatusMovedPermanently)
	}).Methods(http.MethodGet)
	r.PathPrefix(base + "/").Handler(h)
}
