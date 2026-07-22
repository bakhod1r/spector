package sample

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type CreateUserReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// listUsers returns every user.
//
// Supports free-text search and a page size.
// specter:tags users
func listUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := r.URL.Query().Get("limit")
	_, _ = q, limit
	out := []User{}
	json.NewEncoder(w).Encode(out)
}

func getUser(w http.ResponseWriter, r *http.Request) {
	var u User
	json.NewEncoder(w).Encode(u)
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(User{})
}

func deleteUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func health(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-Token")
	_ = token
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func catchAll(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func requestID(next http.Handler) http.Handler { return next }

// tenantGuard is named after nothing in particular: what it demands is only
// visible in its body.
func tenantGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tenant-Key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Register(r *mux.Router) {
	r.Use(requestID)

	api := r.PathPrefix("/api").Subrouter()
	v1 := api.PathPrefix("/v1").Subrouter()
	v1.Use(tenantGuard)

	v1.HandleFunc("/users", listUsers).Methods("GET")
	v1.HandleFunc("/users/{id:[0-9]+}", getUser).Methods("GET")
	v1.HandleFunc("/users", createUser).Methods(http.MethodPost)
	v1.HandleFunc("/users/{id}", deleteUser).Methods("DELETE")

	r.HandleFunc("/health", health)
	r.HandleFunc("/dual", catchAll).Methods("GET", "POST")
}
