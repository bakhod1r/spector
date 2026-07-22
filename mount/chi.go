package mount

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/specter"
)

// Chi registers the console on a chi router under cfg.BasePath
// (default "/docs").
//
// chi routes the whole subtree in one call, so unlike gin and echo there is no
// per-endpoint registration here: anything the console adds later is served
// without touching this function.
func Chi(r chi.Router, cfg specter.Config) {
	base, h := prepare(cfg)

	r.Get(base, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, redirectTarget(base, req.URL.RawQuery), http.StatusMovedPermanently)
	})
	r.Handle(base+"/*", h)
}
