package edge

import "net/http"

// Fixture exercising stdlib adapter corners: wrong arities, dynamic patterns,
// unknown methods, mounts without a nameable handler, served-handler wrapping,
// and non-transparent http.* wrappers.

func setup() {
	mux := newMux()
	mux.HandleFunc("GET /three", h, extra)  // three args, not a registration shape
	mux.HandleFunc(patternVar, h)           // pattern is not a string literal
	mux.HandleFunc("TRACE /t", h)           // method not in the table
	mux.Handle(prefixVar, sub)              // mount prefix is not a string literal
	mux.Handle("/lit/", wrap(42))           // wrapped value has no handler name
	mux.HandleFunc("GET /a", a.b.C(h))      // wrapper fun whose X is a selector
	mux.HandleFunc("GET /b", http.Foo(h))   // http.* call that is not plumbing

	http.ListenAndServe(":8080", logging(mux))
	srv := &http.Server{Handler: logging(mux)}
	srv.ListenAndServe()
	http.ListenAndServeTLS(":8443", "cert", "key", logging(mux))
}

func h(w http.ResponseWriter, r *http.Request) {}
