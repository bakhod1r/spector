package edge

// Fixture exercising gin adapter corners: method-declared handlers, inline
// middleware, dynamic paths and group prefixes, non-ident receivers.

type User struct{ Name string }

type A struct{}

type B struct{}

func (A) dup() User { return User{} }

func (B) dup() User { return User{} } // same method name on a second type

func (A) methodOnly() User { return User{} } // no plain function shares this name

func setup() {
	r := newEngine()
	plain()                     // call.Fun is not a selector
	r.GET(pathVar, h)           // path is not a string literal
	r.GET("/mw", mwFn, h)       // inline middleware before the handler
	s.router.GET("/nested", h)  // receiver is a selector, not an ident
	s.g = r.Group("/x")         // group assigned to a non-ident lhs
	g := r.Group(pathVar)       // group with a dynamic prefix
	g.GET("/inside", h)
	r.GET("/m", methodOnly)
}

func h(c *Ctx) {}
