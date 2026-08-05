package main

import "github.com/gin-gonic/gin"

const userPath = "/users/:id"

var base = "/api/v1"

func main() {
	r := gin.Default()
	r.GET(userPath, getUser)
	r.GET(base+"/health", health)
}

func getUser(c *gin.Context) {}
func health(c *gin.Context)  {}
