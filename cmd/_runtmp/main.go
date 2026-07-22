package main

import (
	"log"
	"net/http"

	"github.com/user/specter"
)

func main() {
	h := specter.Handler(specter.Config{
		Dir:      "examples/shop",
		ProtoDir: "examples/shop/shoppb", // gRPC from generated *.pb.go
		Title:    "Shop API",
		Version:  "1.0.0",
	})
	log.Println("specter console: http://localhost:8099/docs/")
	log.Fatal(http.ListenAndServe(":8099", h))
}
