package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

type User struct {
	ID int `json:"id"`
}

// RequestLogger wraps the whole router where it is served.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// AuthRequired rejects requests without a bearer token.
func AuthRequired(next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next(w, r, ps)
	}
}

// RateLimit caps how often a client may call an endpoint.
func RateLimit(next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		next(w, r, ps)
	}
}

// status is a public health check.
func status(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {}

// listUsers returns every user.
func listUsers(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	json.NewEncoder(w).Encode([]User{})
}

// createUser adds a user.
func createUser(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(User{})
}

func Serve() {
	r := httprouter.New()
	r.GET("/health", status)
	r.GET("/users", AuthRequired(listUsers))
	// Two wrappers: the outer one runs first.
	r.POST("/users", RateLimit(AuthRequired(createUser)))
	// The router itself is wrapped where it is served, so RequestLogger runs in
	// front of every route above.
	http.ListenAndServe(":8080", RequestLogger(r))
}
