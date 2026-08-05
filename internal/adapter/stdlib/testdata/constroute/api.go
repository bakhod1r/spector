package main

import "net/http"

const userPath = "GET /users/{id}"

var base = "GET /api"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc(userPath, getUser)
	mux.HandleFunc(base+"/health", health)
}

func getUser(w http.ResponseWriter, r *http.Request) {}
func health(w http.ResponseWriter, r *http.Request)  {}
