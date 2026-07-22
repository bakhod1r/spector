package edge

import "net/http"

// Fixture exercising echo adapter corners: dynamic paths, Match variants,
// group oddities, bare-star wildcards.

func setup() {
	e := newEcho()
	e.GET(pathVar, h)                                            // path is not a string literal
	e.Match(methodsVar, "/mv", h)                                // methods arg is not a composite literal
	e.Match([]string{"GET", f(), "BOGUS"}, "/m2", h)             // non-literal element and unknown method
	e.Match([]string{http.MethodGet, http.MethodPost}, "/m3", h) // selector method constants
	s.g = e.Group("/x")                                          // group assigned to a non-ident lhs
	g := e.Group(pathVar)                                        // group with a dynamic prefix
	g.GET("/inside", h)
	e.GET("/files/*", h)   // bare * wildcard segment
	e.GET("/dl/*path", h)  // named * wildcard segment
}

func h(c Context) error { return nil }
