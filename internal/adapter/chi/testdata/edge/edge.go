package edge

// Fixture exercising the odd corners of the chi walker: non-selector calls,
// dynamic paths, malformed groups, Mount, and receivers that are calls.

func register() {
	r := newRouter()
	plain() // call.Fun is not a selector

	r.Mount("/m", sub)                        // neither Route nor Group nor a method
	r.Get(pathVar, getThing)                  // path is not a string literal
	r.Route(pathVar, func(r Router) {})       // Route with a dynamic path
	r.Route("/api", notALit)                  // Route whose body is not a func literal
	r.Group(notALit)                          // Group whose arg is not a func literal
	r.Group(func(r Router) {                  // Group happy path
		r.Get("/g", getThing)
	})
	f().Get("/w", getThing)      // receiver is a call that is not With(...)
	r.With(mw).Get("/with", getThing)
}
