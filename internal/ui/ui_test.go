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
		"grpcConnect", "grpcSend", "grpcHalfClose", "grpcCancel",
	}
	for _, id := range ids {
		if !strings.Contains(page, `id="`+id+`"`) {
			t.Errorf("no element with id=%q, but the script queries it", id)
		}
	}
}

// The gRPC panel must open a live WebSocket to grpc/stream and speak the frame
// protocol the Go side implements. Renaming either silently breaks streaming.
func TestGrpcStreamContract(t *testing.T) {
	page := string(Page)
	for _, want := range []string{
		`grpc/stream`,
		`"halfClose"`,
		`"cancel"`,
		`"init"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("gRPC streaming page missing %s", want)
		}
	}
}

// The import/export format is written to disk by users. Renaming either
// constant silently invalidates every file they already exported.
func TestExportFormatConstants(t *testing.T) {
	page := string(Page)
	for _, want := range []string{
		`const EXPORT_FORMAT = "spector.collection"`,
		`const EXPORT_VERSION = 1`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %s — exported files would stop being readable", want)
		}
	}
}

// Run all rows expand to show the request and response. The renderer and the
// per-row detail machinery are referenced by name across the three runners, so
// a rename that missed one would silently drop the panel — pin the contract.
func TestRunAllDetailContract(t *testing.T) {
	page := string(Page)
	for _, want := range []string{
		"function makeDetail(",
		"function renderBody(",
		"RUN_BODY_CAP",
		"run-detail",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("Run all detail view missing %s", want)
		}
	}
}

// An operation card puts the request and its Send controls first, and the
// documented response models below them in a collapsed section. The order is
// the feature: a rename or a stray move would put a screen of models back
// between the body editor and Send.
func TestOperationSectionOrder(t *testing.T) {
	page := string(Page)
	for _, want := range []string{"function opSection(", "opsec-body", "Expected responses ("} {
		if !strings.Contains(page, want) {
			t.Errorf("operation card missing %s", want)
		}
	}
	send := strings.Index(page, `sendBtn.onclick = () => send(req, out);`)
	models := strings.Index(page, `const sec = opSection("Expected responses (`)
	if send < 0 || models < 0 {
		t.Fatal("could not find both the send wiring and the responses section")
	}
	if models < send {
		t.Error("the response models are built before the send controls; they belong below them")
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
