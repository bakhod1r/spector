package localprefix

import "net/http"

func Routes(mux *http.ServeMux) {
	base := "/v1"
	mux.HandleFunc("GET "+base+"/categories", listCategories)
}

func listCategories(w http.ResponseWriter, r *http.Request) {}
