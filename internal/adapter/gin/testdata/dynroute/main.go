package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	paths := []string{"/a", "/b"}
	for _, p := range paths {
		r.GET(p, h) // dynamic: path is a range variable
	}
}

func h(c *gin.Context) {}
