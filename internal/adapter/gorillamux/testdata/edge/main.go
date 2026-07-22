package edge

import (
	"net/http"

	"github.com/gorilla/mux"
)

var dynamicPath = "/dynamic"

var pvar = "/varprefix"

func ok(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func Register(r *mux.Router) {
	// Path is a variable, not a string literal — the scanner must skip it.
	r.HandleFunc(dynamicPath, ok)
	r.HandleFunc(dynamicPath, ok).Methods("GET")

	// Methods on an identifier, not on a registration call.
	route := r.HandleFunc("/split", ok)
	route.Methods("GET")

	// Methods argument that is neither a string literal nor a selector.
	m := "GET"
	r.HandleFunc("/vararg", ok).Methods(m)

	// Subrouter assigned into a map index — LHS is not an identifier.
	subs := map[string]*mux.Router{}
	subs["a"] = r.PathPrefix("/mapped").Subrouter()

	// Subrouter whose receiver is an identifier, not a PathPrefix call.
	plain := r.Subrouter()
	plain.HandleFunc("/plain", ok).Methods("GET")

	// Subrouter built from a chain that is not PathPrefix.
	hdr := r.Headers("X-Edge", "1").Subrouter()
	hdr.HandleFunc("/hdr", ok).Methods("GET")

	// PathPrefix argument that is not a string literal.
	vp := r.PathPrefix(pvar).Subrouter()
	vp.HandleFunc("/vp", ok).Methods("GET")
}
