package main

import (
	"log"
	"net/http"

	"github.com/bakhod1r/spector"
)

func main() {
	h := spector.Handler(spector.Config{
		Dir:      "examples/shop",
		ProtoDir: "examples/shop/shoppb", // gRPC from generated *.pb.go
		Title:    "Shop API",
		Version:  "1.0.0",
		Mock:     true, // documented paths answer from the mock on this origin
	})
	log.Println("spector console: http://localhost:8099/docs/")
	log.Fatal(http.ListenAndServe(":8099", h))
}
