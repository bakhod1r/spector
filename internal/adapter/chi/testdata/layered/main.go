package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/layered/orders"
	"example.com/layered/users"
)

func main() {
	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		users.NewHandler().Mount(api)
		orders.NewHandler().Mount(api)
	})
	http.ListenAndServe(":8080", r)
}
