package sample

import (
	"encoding/json"
	"net/http"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CreateUserReq struct {
	Name string `json:"name"`
}

func logging(next http.Handler) http.Handler { return next }

func apiKeyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == "" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminOnly(next http.HandlerFunc) http.HandlerFunc { return next }

func Register(mux *http.ServeMux) {
	v1 := http.NewServeMux()
	v1.HandleFunc("GET /users", listUsers)
	v1.HandleFunc("GET /users/{id}", getUser)
	v1.HandleFunc("POST /users", createUser)
	v1.HandleFunc("DELETE /users/{id}", adminOnly(deleteUser))
	mux.Handle("/api/v1/", apiKeyGuard(http.StripPrefix("/api/v1", v1)))

	mux.HandleFunc("GET /health", health)
}

func Serve(mux *http.ServeMux) error {
	return http.ListenAndServe(":8080", logging(mux))
}

// listUsers returns every user.
// specter:tags users
func listUsers(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("limit")
	_ = r.Header.Get("X-Tenant")
	out := []User{}
	json.NewEncoder(w).Encode(out)
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func deleteUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func getUser(w http.ResponseWriter, r *http.Request) {
	var u User
	json.NewEncoder(w).Encode(u)
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserReq
	json.NewDecoder(r.Body).Decode(&req)
	json.NewEncoder(w).Encode(&User{})
}
