package sample

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CreateUserReq struct {
	Name string `json:"name"`
}

func requestID(next http.Handler) http.Handler { return next }

func apiKeyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == "" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminOnly(next http.Handler) http.Handler { return next }

func Register(r chi.Router) {
	r.Use(requestID)

	// Registered before the guard, so it must not inherit it.
	r.Get("/health", health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(apiKeyGuard)

		r.Get("/users", listUsers)
		r.Get("/users/{id}", getUser)
		r.Post("/users", createUser)

		r.With(adminOnly).Delete("/users/{id}", deleteUser)
	})

	// A sibling subtree must not pick up the other one's middleware.
	r.Route("/public", func(r chi.Router) {
		r.Get("/status", health)
	})
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
