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

func Register(mux *http.ServeMux) {
	v1 := http.NewServeMux()
	v1.HandleFunc("GET /users", listUsers)
	v1.HandleFunc("GET /users/{id}", getUser)
	v1.HandleFunc("POST /users", createUser)
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", v1))
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
