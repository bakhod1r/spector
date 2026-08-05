package main

import "testing"

func TestPackageName(t *testing.T) {
	cases := map[string]string{
		"./out/Admin-Panel": "adminpanel",
		"gen":               "gen",
		"123abc":            "admin",
		"---":               "admin",
	}
	for in, want := range cases {
		if got := packageName(in); got != want {
			t.Errorf("packageName(%q) = %q, want %q", in, got, want)
		}
	}
}
