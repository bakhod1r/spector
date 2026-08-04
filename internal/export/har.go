package export

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/user/specter/internal/core"
	"github.com/user/specter/internal/mock"
)

// HAR 1.2 (http://www.softwareishard.com/blog/har-12-spec/) — the log format
// browsers, proxies and load tools read. Exporting the document as a HAR turns
// a spec into a replayable archive of example calls: one entry per operation,
// its body sampled from the request schema and its response from the documented
// success schema.

type harFile struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int         `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
}

type harTimings struct {
	Send    int `json:"send"`
	Wait    int `json:"wait"`
	Receive int `json:"receive"`
}

type harRequest struct {
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	HTTPVersion string       `json:"httpVersion"`
	Headers     []harNV      `json:"headers"`
	QueryString []harNV      `json:"queryString"`
	Cookies     []harNV      `json:"cookies"`
	PostData    *harPostData `json:"postData,omitempty"`
	HeadersSize int          `json:"headersSize"`
	BodySize    int          `json:"bodySize"`
}

type harResponse struct {
	Status      int        `json:"status"`
	StatusText  string     `json:"statusText"`
	HTTPVersion string     `json:"httpVersion"`
	Headers     []harNV    `json:"headers"`
	Cookies     []harNV    `json:"cookies"`
	Content     harContent `json:"content"`
	RedirectURL string     `json:"redirectURL"`
	HeadersSize int        `json:"headersSize"`
	BodySize    int        `json:"bodySize"`
}

type harContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

type harPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type harNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// A fixed timestamp keeps the export byte-identical across runs; a HAR requires
// the field but the value carries no meaning for a spec-derived archive.
const harEpoch = "1970-01-01T00:00:00.000Z"

// HAR renders the document as a HAR 1.2 archive. The base URL comes from the
// first server; with none, URLs stay relative rather than invented.
func HAR(doc *core.Document) ([]byte, error) {
	base := ""
	if len(doc.Servers) > 0 {
		base = strings.TrimRight(doc.Servers[0].URL, "/")
	}
	log := harFile{Log: harLog{
		Version: "1.2",
		Creator: harCreator{Name: "specter", Version: "1"},
		Entries: []harEntry{},
	}}
	for _, path := range sortedPaths(doc) {
		for _, method := range sortedMethods(doc.Paths[path]) {
			op := doc.Paths[path][method]
			log.Log.Entries = append(log.Log.Entries, harEntryFor(doc, base, path, method, op))
		}
	}
	return json.MarshalIndent(log, "", "  ")
}

func harEntryFor(doc *core.Document, base, path, method string, op *core.Operation) harEntry {
	req := harRequest{
		Method:      strings.ToUpper(method),
		URL:         base + path,
		HTTPVersion: "HTTP/1.1",
		Headers:     []harNV{},
		QueryString: []harNV{},
		Cookies:     []harNV{},
		HeadersSize: -1,
		BodySize:    -1,
	}

	for _, p := range op.Parameters {
		param := resolveParam(doc, p)
		switch param.In {
		case "query":
			req.QueryString = append(req.QueryString, harNV{Name: param.Name, Value: ""})
		case "header":
			req.Headers = append(req.Headers, harNV{Name: param.Name, Value: ""})
		}
	}

	if op.RequestBody != nil {
		if media, ok := op.RequestBody.Content["application/json"]; ok && media.Schema != nil {
			if body, err := json.MarshalIndent(mock.Sample(doc, media.Schema, nil), "", "  "); err == nil {
				req.Headers = append(req.Headers, harNV{Name: "Content-Type", Value: "application/json"})
				req.PostData = &harPostData{MimeType: "application/json", Text: string(body)}
				req.BodySize = len(body)
			}
		}
	}

	return harEntry{
		StartedDateTime: harEpoch,
		Time:            0,
		Request:         req,
		Response:        harResponseFor(doc, op),
		Timings:         harTimings{Send: 0, Wait: 0, Receive: 0},
	}
}

// harResponseFor picks the lowest documented status as the representative
// response and seeds its body from the schema. With no documented response it
// falls back to a bare 200 so the entry stays valid.
func harResponseFor(doc *core.Document, op *core.Operation) harResponse {
	resp := harResponse{
		Status:      200,
		StatusText:  "OK",
		HTTPVersion: "HTTP/1.1",
		Headers:     []harNV{},
		Cookies:     []harNV{},
		Content:     harContent{MimeType: "application/json"},
		HeadersSize: -1,
		BodySize:    -1,
	}
	codes := make([]string, 0, len(op.Responses))
	for code := range op.Responses {
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return resp
	}
	// sortedPaths reuses sort; codes sort lexically, which orders "200" before
	// "404" for the numeric range we emit.
	lowest := codes[0]
	for _, c := range codes {
		if c < lowest {
			lowest = c
		}
	}
	if n, err := strconv.Atoi(lowest); err == nil {
		resp.Status = n
		if txt := http.StatusText(n); txt != "" {
			resp.StatusText = txt
		}
	}
	if r := op.Responses[lowest]; r != nil {
		if media, ok := r.Content["application/json"]; ok && media.Schema != nil {
			if body, err := json.MarshalIndent(mock.Sample(doc, media.Schema, nil), "", "  "); err == nil {
				resp.Content.Text = string(body)
				resp.Content.Size = len(body)
				resp.BodySize = len(body)
				resp.Headers = append(resp.Headers, harNV{Name: "Content-Type", Value: "application/json"})
			}
		}
	}
	return resp
}
