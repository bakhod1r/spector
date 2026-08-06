package localprefix

import "github.com/go-chi/chi/v5"

func Routes(r chi.Router) {
	base := "/v1"
	r.Get(base+"/categories", nil)
}
