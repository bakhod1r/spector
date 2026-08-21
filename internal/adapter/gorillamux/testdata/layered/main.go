package main

import (
	"net/http"

	"github.com/gorilla/mux"

	"example.com/layered/orders"
	"example.com/layered/users"
)

func main() {
	r := mux.NewRouter()
	api := r.PathPrefix("/api/v1").Subrouter()
	users.NewHandler().Mount(api)
	orders.NewHandler().Mount(api)
	http.ListenAndServe(":8080", r)
}
