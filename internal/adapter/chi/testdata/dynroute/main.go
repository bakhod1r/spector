package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func Register(r chi.Router) {
	paths := []string{"/a", "/b"}
	for _, p := range paths {
		r.Get(p, h) // dynamic: path is a range variable
	}
}

func h(w http.ResponseWriter, r *http.Request) {}
