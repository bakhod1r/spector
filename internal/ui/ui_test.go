package ui

import (
	"strings"
	"testing"
)

// The console is a single embedded HTML file with no build step, so nothing
// catches a broken embed or a control that lost its handler. These tests pin
// the contract between the Go side and the page.

func TestPageIsEmbedded(t *testing.T) {
	if len(Page) == 0 {
		t.Fatal("Page is empty: the //go:embed of ui.html did not resolve")
	}
	if !strings.Contains(string(Page), "<html") {
		t.Error("Page does not look like an HTML document")
	}
}

// Every element the script reaches for by id must exist in the markup.
// A typo in either place is a silent null-deref at runtime.
func TestControlsHaveMarkup(t *testing.T) {
	page := string(Page)
	ids := []string{
		"title", "ver", "envSel", "envManage", "search", "themeBtn",
		"tabRest", "tabGrpc", "tabGraphql", "tabRealtime",
		"collPane", "histPane", "envOverlay", "envClose",
		"catNav", "catSect", "collGroup", "histGroup",
		"exportBtn", "importBtn", "importFile",
	}
	for _, id := range ids {
		if !strings.Contains(page, `id="`+id+`"`) {
			t.Errorf("no element with id=%q, but the script queries it", id)
		}
	}
}

// The import/export format is written to disk by users. Renaming either
// constant silently invalidates every file they already exported.
func TestExportFormatConstants(t *testing.T) {
	page := string(Page)
	for _, want := range []string{
		`const EXPORT_FORMAT = "specter.collection"`,
		`const EXPORT_VERSION = 1`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %s — exported files would stop being readable", want)
		}
	}
}

// The page fetches these from the handler; the handler must keep serving them.
func TestFetchedEndpoints(t *testing.T) {
	page := string(Page)
	for _, path := range []string{"openapi.json", "grpc.json", "graphql.json"} {
		if !strings.Contains(page, `fetch("`+path+`")`) {
			t.Errorf("page no longer fetches %s", path)
		}
	}
}
