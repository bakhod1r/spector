package sample

import (
	"encoding/json"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CreateUserReq struct {
	Name string `json:"name"`
}

// listUsers returns every user.
func listUsers(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	json.NewEncoder(w).Encode([]User{})
}

// getUser returns one user by id.
func getUser(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	json.NewEncoder(w).Encode(User{})
}

// createUser adds a user.
func createUser(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req CreateUserReq
	json.NewDecoder(r.Body).Decode(&req)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(User{})
}

// deleteUser removes a user.
func deleteUser(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	w.WriteHeader(http.StatusNoContent)
}

func Router() *httprouter.Router {
	r := httprouter.New()
	r.GET("/api/v1/users", listUsers)
	r.GET("/api/v1/users/:id", getUser)
	r.POST("/api/v1/users", createUser)
	// The generic form: method is an argument, not the selector.
	r.Handle("DELETE", "/api/v1/users/:id", deleteUser)
	// A catch-all segment normalises to a braced parameter.
	r.GET("/files/*filepath", listUsers)
	return r
}
