package localprefix

import "github.com/julienschmidt/httprouter"

func Routes(r *httprouter.Router) {
	base := "/v1"
	r.GET(base+"/categories", nil)
}
