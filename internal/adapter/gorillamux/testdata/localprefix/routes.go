package localprefix

import "github.com/gorilla/mux"

func Routes(r *mux.Router) {
	base := "/v1"
	r.HandleFunc(base+"/categories", nil).Methods("GET")
}
