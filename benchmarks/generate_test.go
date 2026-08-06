// Package benchmarks measures the cost of the hot paths a large repository
// hits: scanning source and generating the document. It lives in its own
// directory so `go test -bench` here does not pull the whole module's test
// binary, and so the numbers are comparable run to run.
//
// Run with:
//
//	go test -bench . -benchmem ./benchmarks/
package benchmarks

import (
	"testing"

	"github.com/user/specter"
)

// shopDir is the example API, reached relative to this package's directory
// (the working directory `go test` uses is the package dir, not the repo root).
const shopDir = "../examples/shop"

// BenchmarkGenerate measures a full OpenAPI generation: parse, scan, resolve
// routes, and build the document.
func BenchmarkGenerate(b *testing.B) {
	cfg := specter.Config{Dir: shopDir, Title: "Shop", Version: "1.0.0"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := specter.Generate(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGenerateGrpc measures the gRPC document path, which parses the
// generated *.pb.go files rather than the router source.
func BenchmarkGenerateGrpc(b *testing.B) {
	cfg := specter.Config{Dir: shopDir, ProtoDir: "../examples/shop/shoppb"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := specter.GenerateGrpc(cfg); err != nil {
			b.Fatal(err)
		}
	}
}
