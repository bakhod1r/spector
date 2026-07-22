package calls

import (
	"go/ast"
	"strings"

	"github.com/user/specter/internal/core"
)

// byPackage maps an import path fragment to the kind of dependency reached
// through it. Going through a package name is the reliable signal: the import
// is right there in the file, so there is nothing to guess.
var byPackage = []struct {
	fragment string
	kind     string
	label    string
}{
	{"database/sql", core.CallDB, "sql"},
	{"gorm.io/gorm", core.CallDB, "gorm"},
	{"jmoiron/sqlx", core.CallDB, "sqlx"},
	{"jackc/pgx", core.CallDB, "pgx"},
	{"go.mongodb.org", core.CallDB, "mongo"},
	{"redis/go-redis", core.CallCache, "redis"},
	{"gomodule/redigo", core.CallCache, "redis"},
	{"bradfitz/gomemcache", core.CallCache, "memcached"},
	{"net/http", core.CallHTTP, "http"},
	{"go-resty/resty", core.CallHTTP, "resty"},
	{"google.golang.org/grpc", core.CallGRPC, "grpc"},
	{"segmentio/kafka-go", core.CallQueue, "kafka"},
	{"Shopify/sarama", core.CallQueue, "kafka"},
	{"IBM/sarama", core.CallQueue, "kafka"},
	{"nats-io/nats", core.CallQueue, "nats"},
	{"rabbitmq/amqp", core.CallQueue, "rabbitmq"},
	{"streadway/amqp", core.CallQueue, "rabbitmq"},
	{"aws/aws-sdk-go", core.CallHTTP, "aws"},
}

// httpFuncs are the net/http functions that actually make a request. The rest
// of the package is servers and plumbing, and reporting http.HandlerFunc as an
// outbound call would be noise on every handler in the program.
var httpFuncs = map[string]bool{
	"Get": true, "Post": true, "Head": true, "PostForm": true, "NewRequest": true,
	"NewRequestWithContext": true,
}

// byReceiverName is the fallback: no package name is involved, so the only
// signal left is what the variable is called. These are conventions, not
// guarantees, so anything matched here is reported as a guess.
var byReceiverName = []struct {
	names []string
	kind  string
}{
	{[]string{"db", "sql", "conn", "tx", "dbx", "database", "repo", "store"}, core.CallDB},
	{[]string{"redis", "cache", "rdb", "memcache"}, core.CallCache},
	{[]string{"client", "http", "api", "httpclient"}, core.CallHTTP},
	// "writer" is ambiguous on its own — io.Writer is everywhere — but the
	// method still has to be a queue method, and io.Writer has no
	// WriteMessages or Publish.
	{[]string{"producer", "publisher", "queue", "kafka", "nats", "amqp", "broker", "writer", "bus"}, core.CallQueue},
}

// dbMethods are the method names that mean a query is being issued. Names like
// Get or Find are excluded on purpose: they appear on maps, caches, and plain
// structs, and matching them turns the dependency map into a list of
// everything.
var dbMethods = map[string]bool{
	"Query": true, "QueryRow": true, "QueryContext": true, "QueryRowContext": true,
	"Exec": true, "ExecContext": true, "Prepare": true, "PrepareContext": true,
	"Begin": true, "BeginTx": true, "Select": true, "NamedExec": true,
}

// requestMethods mean an outbound request is being sent.
var requestMethods = map[string]bool{
	"Do": true, "Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true,
	"Send": true, "Head": true,
}

// queueMethods mean a message is being produced.
var queueMethods = map[string]bool{
	"Publish": true, "Produce": true, "Send": true, "SendMessage": true,
	"WriteMessages": true, "Enqueue": true,
}

// classify decides whether a call reaches out of the process, and how sure we
// are. It returns false for the overwhelming majority of calls, which is the
// point: only the ones that leave the process are worth drawing.
func classify(call *ast.CallExpr, imports map[string]string) (core.Call, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return core.Call{}, false
	}
	recvName, isPlainIdent := receiverName(sel.X)
	if recvName == "" {
		return core.Call{}, false
	}
	method := sel.Sel.Name

	// 1. Through a package name: the import says what this is. Only a bare
	// identifier can be a package name; s.db is a field, whatever s is.
	if path, isImport := imports[recvName]; isImport && isPlainIdent {
		for _, p := range byPackage {
			if !strings.Contains(path, p.fragment) {
				continue
			}
			if p.label == "http" && !httpFuncs[method] {
				return core.Call{}, false // net/http is mostly server-side
			}
			return core.Call{
				Kind:       p.kind,
				Target:     p.label + "." + method,
				Confidence: core.Certain,
			}, true
		}
		return core.Call{}, false
	}

	// 2. Through a receiver name: a convention, reported as a guess.
	lower := strings.ToLower(recvName)
	for _, r := range byReceiverName {
		if !matchesName(lower, r.names) {
			continue
		}
		if !methodFits(r.kind, method) {
			continue
		}
		return core.Call{
			Kind:       r.kind,
			Target:     recvName + "." + method,
			Confidence: core.Likely,
		}, true
	}
	return core.Call{}, false
}

// receiverName reduces a call's receiver to the name worth judging, and reports
// whether it was a bare identifier.
//
// Dependencies are usually struct fields — s.db.Query, srv.cache.Get — so a
// receiver of s.db must be read as "db". Only a bare identifier can name an
// imported package, which is why the caller needs to know which it was.
// Anything deeper (a call, an index, a chain) has no name to judge and is left
// to the walk, which will see the inner call on its own.
func receiverName(x ast.Expr) (name string, plain bool) {
	switch t := x.(type) {
	case *ast.Ident:
		return t.Name, true
	case *ast.SelectorExpr:
		return t.Sel.Name, false
	}
	return "", false
}

// methodFits requires the method to look like the operation the receiver name
// suggests. Without it, `db.String()` would be reported as a query.
func methodFits(kind, method string) bool {
	switch kind {
	case core.CallDB:
		return dbMethods[method]
	case core.CallCache:
		return method == "Get" || method == "Set" || method == "Del" ||
			method == "SetEX" || method == "Expire" || method == "MGet"
	case core.CallHTTP:
		return requestMethods[method]
	case core.CallQueue:
		return queueMethods[method]
	}
	return false
}

// matchesName accepts an exact name or one with a conventional suffix, so both
// `db` and `userDB` are recognised while `dbg` is not.
func matchesName(lower string, names []string) bool {
	for _, n := range names {
		if lower == n || strings.HasSuffix(lower, n) {
			return true
		}
	}
	return false
}
