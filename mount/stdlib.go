package mount

import (
	"net/http"

	"github.com/user/specter"
)

// Stdlib registers the console on a net/http mux under cfg.BasePath
// (default "/docs").
//
// A pattern ending in "/" is already a subtree match in the standard mux, and
// the mux itself redirects "/docs" to "/docs/" — but it drops the query when it
// does, which would lose an access key on the first visit, so the bare path is
// registered explicitly.
func Stdlib(mux *http.ServeMux, cfg specter.Config) {
	base, h := prepare(cfg)

	mux.HandleFunc(base, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, redirectTarget(base, req.URL.RawQuery), http.StatusMovedPermanently)
	})
	mux.Handle(base+"/", h)
}
