package sample

import (
	"encoding/json"
	"net/http"

	"github.com/uptrace/bunrouter"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CreateUserReq struct {
	Name string `json:"name"`
}

// listUsers returns every user.
func listUsers(w http.ResponseWriter, req bunrouter.Request) error {
	return json.NewEncoder(w).Encode([]User{})
}

// getUser returns one user by id.
func getUser(w http.ResponseWriter, req bunrouter.Request) error {
	return json.NewEncoder(w).Encode(User{})
}

// createUser adds a user.
func createUser(w http.ResponseWriter, req bunrouter.Request) error {
	var body CreateUserReq
	json.NewDecoder(req.Body).Decode(&body)
	w.WriteHeader(http.StatusCreated)
	return json.NewEncoder(w).Encode(User{})
}

// deleteUser removes a user.
func deleteUser(w http.ResponseWriter, req bunrouter.Request) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// status is a bare health check outside any group.
func status(w http.ResponseWriter, req bunrouter.Request) error {
	return nil
}

func Router() *bunrouter.Router {
	r := bunrouter.New()
	r.GET("/health", status)
	r.WithGroup("/api/v1", func(g *bunrouter.Group) {
		g.GET("/users", listUsers)
		g.GET("/users/:id", getUser)
		g.POST("/users", createUser)
		g.DELETE("/users/:id", deleteUser)
		// A wildcard segment normalises to a braced parameter.
		g.GET("/files/*path", listUsers)
	})
	return r
}
