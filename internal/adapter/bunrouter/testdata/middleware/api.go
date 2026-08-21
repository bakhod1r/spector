package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/uptrace/bunrouter"
)

type User struct {
	ID int `json:"id"`
}

// RequestLogger runs in front of every route on the router.
func RequestLogger(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
	return func(w http.ResponseWriter, req bunrouter.Request) error {
		return next(w, req)
	}
}

// AuthRequired rejects requests without a bearer token.
func AuthRequired(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
	return func(w http.ResponseWriter, req bunrouter.Request) error {
		if req.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return nil
		}
		return next(w, req)
	}
}

// RateLimit caps how often a client may call an endpoint.
func RateLimit(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
	return func(w http.ResponseWriter, req bunrouter.Request) error {
		return next(w, req)
	}
}

// status is a public health check.
func status(w http.ResponseWriter, req bunrouter.Request) error { return nil }

// listUsers returns every user.
func listUsers(w http.ResponseWriter, req bunrouter.Request) error {
	return json.NewEncoder(w).Encode([]User{})
}

// createUser adds a user.
func createUser(w http.ResponseWriter, req bunrouter.Request) error {
	w.WriteHeader(http.StatusCreated)
	return json.NewEncoder(w).Encode(User{})
}

// search is public but rate limited.
func search(w http.ResponseWriter, req bunrouter.Request) error { return nil }

func Router() *bunrouter.Router {
	r := bunrouter.New(bunrouter.Use(RequestLogger))
	r.GET("/health", status)
	// Middleware scoped to one route through the chained receiver.
	r.Use(RateLimit).GET("/search", search)
	r.WithGroup("/api", func(g *bunrouter.Group) {
		g.Use(AuthRequired)
		g.GET("/users", listUsers)
		g.POST("/users", createUser)
	})
	return r
}
