package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

const userPath = "/users/{id}"

var base = "/api/v1"

func Register(r chi.Router) {
	r.Get(userPath, getUser)
	r.Get(base+"/health", health)
}

func getUser(w http.ResponseWriter, r *http.Request) {}
func health(w http.ResponseWriter, r *http.Request)  {}
