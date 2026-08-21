package main

import (
	"net/http"

	"github.com/julienschmidt/httprouter"

	"example.com/layered/orders"
	"example.com/layered/users"
)

func main() {
	r := httprouter.New()
	users.NewHandler().Mount(r)
	orders.NewHandler().Mount(r)
	http.ListenAndServe(":8080", r)
}
