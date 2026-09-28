package proxy

import "testing"

func FuzzNormalisePath(f *testing.F) {
	for _, s := range []string{"/users/123", "/a/550e8400-e29b-41d4-a716-446655440000", "", "//x//", "/%zz"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		once := NormalisePath(p)
		if twice := NormalisePath(once); twice != once {
			t.Fatalf("NormalisePath not idempotent: %q -> %q -> %q", p, once, twice)
		}
	})
}

func FuzzRedactQuery(f *testing.F) {
	f.Add("api_key=x&page=2")
	f.Add("%zz=1&token=a;b")
	f.Fuzz(func(t *testing.T, q string) { _ = redactQuery(q) })
}
