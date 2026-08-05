package main

import "github.com/labstack/echo/v4"

const userPath = "/users/:id"

var base = "/api/v1"

func main() {
	e := echo.New()
	e.GET(userPath, getUser)
	e.GET(base+"/health", health)
}

func getUser(c echo.Context) error { return nil }
func health(c echo.Context) error  { return nil }
