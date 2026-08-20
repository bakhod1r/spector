package main

import (
	"net/http"

	"example.com/layered/orders"
	"example.com/layered/users"
)

func main() {
	mux := http.NewServeMux()
	users.NewHandler().Mount(mux)
	orders.NewHandler().Mount(mux)
	http.ListenAndServe(":8080", mux)
}
