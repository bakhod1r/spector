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

func Register(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/users", listUsers)
		r.Get("/users/{id}", getUser)
		r.Post("/users", createUser)
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

func getUser(w http.ResponseWriter, r *http.Request) {
	var u User
	json.NewEncoder(w).Encode(u)
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserReq
	json.NewDecoder(r.Body).Decode(&req)
	json.NewEncoder(w).Encode(&User{})
}
