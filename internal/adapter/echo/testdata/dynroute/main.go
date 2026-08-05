package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	paths := []string{"/a", "/b"}
	for _, p := range paths {
		e.GET(p, h) // dynamic: path is a range variable
	}
}

func h(c echo.Context) error { return nil }
