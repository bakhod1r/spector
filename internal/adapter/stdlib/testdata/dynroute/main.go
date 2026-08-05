package main

import "net/http"

func main() {
	mux := http.NewServeMux()
	paths := []string{"GET /a", "GET /b"}
	for _, p := range paths {
		mux.HandleFunc(p, h) // dynamic: path is a range variable
	}
}

func h(w http.ResponseWriter, r *http.Request) {}
