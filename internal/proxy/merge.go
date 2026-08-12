package proxy

import (
	"encoding/json"
	"sort"

	"github.com/user/specter/internal/core"
)

// MergeObserved returns base with observed traffic folded in: an operation the
// base lacks is added whole and marked observed; an operation the base already
// documents gains only the status codes it did not already carry — an existing
// response is never overwritten, so the source spec wins per response. Neither
// input is mutated.
//
// This is how the dynamic-route gaps (routes the AST could not resolve, which
// are absent from base and emit a diagnostic) get filled from live traffic, and
// the single implementation behind both the proxy's -proxy-merge output and the
// offline -merge-learned command.
func MergeObserved(base, observed *core.Document) *core.Document {
	out := cloneDoc(base)
	if observed == nil {
		return out
	}

	for _, path := range sortedKeys(observed.Paths) {
		methods := observed.Paths[path]
		for _, method := range sortedKeys(methods) {
			oop := methods[method]
			existing := out.Paths[path][method]
			if existing == nil {
				cp := cloneOp(oop)
				cp.Observed = true
				out.AddOperation(path, method, cp)
				continue
			}
			// Enrich: append only the statuses the base did not document.
			if existing.Responses == nil {
				existing.Responses = map[string]*core.Response{}
			}
			for code, resp := range oop.Responses {
				if _, have := existing.Responses[code]; !have {
					existing.Responses[code] = cloneResponse(resp)
				}
			}
		}
	}
	return out
}

// sortedKeys returns a map's keys in sorted order so the merge is deterministic.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// cloneDoc deep-copies a document via a JSON round-trip. Diagnostics are
// excluded from JSON (json:"-"), so they are reattached explicitly — the merged
// document should still carry the base's unresolved-route diagnostics.
func cloneDoc(d *core.Document) *core.Document {
	if d == nil {
		return core.NewDocument("", "")
	}
	var out core.Document
	roundTrip(d, &out)
	out.Diagnostics = append([]core.Diagnostic(nil), d.Diagnostics...)
	return &out
}

func cloneOp(op *core.Operation) *core.Operation {
	var out core.Operation
	roundTrip(op, &out)
	return &out
}

func cloneResponse(r *core.Response) *core.Response {
	var out core.Response
	roundTrip(r, &out)
	return &out
}

func roundTrip(from, to any) {
	data, _ := json.Marshal(from)
	_ = json.Unmarshal(data, to)
}
