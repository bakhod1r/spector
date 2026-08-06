package localprefix

import "github.com/uptrace/bunrouter"

func Routes(r *bunrouter.Router) {
	base := "/v1"
	r.GET(base+"/categories", nil)
}
