package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	// Aliased: `fs` is the flag set inside run, and one meaning per name is
	// worth more than the shorter import.
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bakhod1r/spector"
	"gopkg.in/yaml.v3"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run holds the whole CLI so it can be exercised without a process boundary:
// streams are injected and failures come back as an exit code rather than a
// call to os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("spector", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dirFlag := fs.String("dir", ".", "directory to scan")
	configPath := fs.String("config", "", "config file, JSON or YAML by extension (default: spector.json, .yaml or .yml in -dir, if present)")
	adapter := fs.String("adapter", "", "framework adapter (gin, chi, echo, fiber, gorillamux, httprouter, bunrouter, stdlib); autodetected if empty")
	title := fs.String("title", "", "API title (defaults to directory name)")
	version := fs.String("version", "0.1.0", "API version")
	out := fs.String("o", "", "output file (defaults to stdout)")
	grpc := fs.Bool("grpc", false, "export the gRPC document (.proto/.pb.go) instead of OpenAPI")
	protoDir := fs.String("proto", "", "directory to scan for gRPC sources (defaults to -dir)")
	graphql := fs.Bool("graphql", false, "export the GraphQL document (.graphql/gqlgen) instead of OpenAPI")
	graphqlDir := fs.String("graphqlDir", "", "directory to scan for GraphQL sources (defaults to -dir)")
	lintOnly := fs.Bool("lint", false, "report routing problems instead of a document; exits 1 if any are found")
	all := fs.Bool("all", false, "write openapi.json, grpc.json and graphql.json into -o (a directory)")
	sdkLang := fs.String("sdk", "", "generate a typed client instead of a document: go, ts, python, js, ruby, php, csharp, rust, kotlin, java")
	sdkOut := fs.String("sdk-out", "", "directory the generated client is written into (default ./sdk)")
	sdkPkg := fs.String("sdk-package", "", "package name for the generated Go client (default: client)")
	openapiIn := fs.String("openapi", "", "generate the -sdk client from this OpenAPI file (.json/.yaml) instead of scanning source")
	watch := fs.Bool("watch", false, "stay running and regenerate whenever the scanned sources change")
	contractOut := fs.String("contract", "", "generate contract artefacts into this directory (e.g. ./contract)")
	contractFormat := fs.String("contract-format", "", "comma-separated: http, go, curl (default: all three)")
	contractAPI := fs.String("contract-api", "", "base URL the artefacts call (default: the document's first server)")
	contractPkg := fs.String("contract-package", "", "package name for the generated Go tests (default: the directory name)")
	evolveMode := fs.Bool("evolve", false, "report how the API changed against a baseline, classified breaking/compatible/addition")
	evolveSince := fs.String("since", "", "compare against a git revision, e.g. HEAD~1 or v1.0.0")
	evolveBaselineDir := fs.String("baseline-dir", "", "compare against another source directory")
	evolveBaseline := fs.String("baseline", "", "compare against an existing openapi.json")
	evolveFormat := fs.String("evolve-format", "text", "evolution report format: text, json, or markdown")
	failOnBreaking := fs.Bool("fail-on-breaking", false, "exit non-zero if any breaking change is found (for CI)")
	proxyAddr := fs.String("proxy", "", "run a verifying proxy on this address (e.g. :8080), forwarding to -proxy-target")
	proxyTarget := fs.String("proxy-target", "", "the real API the proxy forwards to (e.g. http://localhost:3000)")
	proxyReport := fs.String("proxy-report", "", "write a JSON drift report to this file on exit")
	proxyRecord := fs.String("proxy-record", "", "record traffic to this file as JSONL; credentials are redacted (see -proxy-record-raw)")
	proxyRecordRaw := fs.Bool("proxy-record-raw", false, "record traffic WITHOUT redacting credentials or masking sensitive fields; the file will contain secrets — never commit it")
	proxyLearn := fs.String("proxy-learn", "", "write an OpenAPI fragment for endpoints seen in traffic but missing from the document")
	proxyMerge := fs.Bool("proxy-merge", false, "with -proxy-learn, write the source document with observed traffic merged in (fills dynamic-route gaps) instead of a bare fragment")
	mergeLearned := fs.String("merge-learned", "", "merge a previously written observed fragment (.json) into the scanned document, filling routes the AST could not resolve")
	proxyStrict := fs.Bool("proxy-strict", false, "exit non-zero if any drift was found (for CI)")
	mockAddr := fs.String("mock", "", "serve the document as a mock API on this address (e.g. :8080)")
	mockOrigins := fs.String("mock-origin", "", "comma-separated origins allowed to call the mock (default any)")
	mockCreds := fs.Bool("mock-credentials", false, "allow cookies and Authorization headers on mock requests")
	mockMaxAge := fs.Int("mock-max-age", 0, "seconds a browser may cache the mock's CORS preflight")
	mcpFlag := fs.Bool("mcp", false, "serve spector as an MCP server over stdio (requires a build with -tags mcp)")
	oasVersion := fs.String("openapi-version", "3.0", "OpenAPI version to emit: 3.0 or 3.1")
	postman := fs.Bool("postman", false, "export a Postman collection v2.1 (Insomnia imports it too)")
	postmanEnv := fs.Bool("postman-env", false, "export a Postman environment (baseUrl and auth placeholders) instead of the collection")
	markdown := fs.Bool("markdown", false, "export static Markdown API docs")
	har := fs.Bool("har", false, "export a HAR 1.2 archive of example calls (one entry per operation)")
	asyncapi := fs.Bool("asyncapi", false, "export an AsyncAPI 2.6 document of the WebSocket and SSE endpoints")
	mockAuth := fs.Bool("mock-auth", false, "mock enforces documented security: missing credentials get 401")
	genTests := fs.String("gen-tests", "", "write a Go integration test file to this path (e.g. ./apitest/api_test.go)")
	testPkg := fs.String("test-package", "", "package name for the generated test file (default: apitest)")
	coverageFlag := fs.Bool("coverage", false, "report documentation coverage instead of a document")
	coverageMin := fs.Float64("coverage-min", 0, "exit 1 when coverage is below this percent (implies -coverage)")
	gateway := fs.Bool("gateway", false, "export a REST document built from google.api.http annotations in .proto sources (gRPC-Gateway)")
	format := fs.String("format", "", "document output format: json (default) or yaml; inferred from -o's extension when empty")
	strictRoutes := fs.Bool("strict-routes", false, "exit non-zero if any route path cannot be statically resolved")
	serveAddr := fs.String("serve", "", "serve the interactive console on this address (e.g. :8099) until stopped")
	serveMock := fs.Bool("serve-mock", false, "with -serve, answer documented paths from the built-in mock on the console origin (adds a MOCK badge)")
	prod := fs.Bool("prod", false, "production mode: hide the scanned source (no file paths, line numbers, or View source) from the document and console")
	// -V, not -version: -version is the version of the API being documented,
	// and has been since the first release. A build that renamed it would
	// silently change what every existing invocation writes into the document.
	showBuild := fs.Bool("V", false, "print the spector build version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showBuild {
		fmt.Fprintln(stdout, buildVersion())
		return 0
	}

	if *mcpFlag {
		return runMCP(stderr)
	}

	cfg := spector.Config{
		Dir:        *dirFlag,
		Adapter:    *adapter,
		Title:      *title,
		Version:    *version,
		ProtoDir:   *protoDir,
		GraphqlDir: *graphqlDir,
	}

	fail := func(err error) int {
		fmt.Fprintln(stderr, "spector:", err)
		return 1
	}

	// Servers and security schemes are declared rather than inferred, and a map
	// of schemes does not fit on a command line. Without the file the CLI's
	// document and the console's disagree about the same API.
	if err := applyConfigFile(&cfg, fs, *configPath, *dirFlag); err != nil {
		return fail(err)
	}
	// A passed -prod always wins; the config file can turn it on by default.
	if *prod {
		cfg.Production = true
	}

	// -serve is a long-lived mode: it runs the console until the process is
	// stopped, so it must not fall through to the one-shot generation path.
	if *serveAddr != "" {
		cfg.Mock = *serveMock
		fmt.Fprintf(stderr, "spector: console on http://localhost%s%s/\n", *serveAddr, cfg.BasePathOrDefault())
		if serr := spector.ServeConsole(*serveAddr, cfg); serr != nil {
			return fail(serr)
		}
		return 0
	}
	// An empty result is not an error: the scan ran, it just found nothing.
	// A warning names the directory so the cause is obvious.
	warnEmpty := func(what, scanDir string) {
		fmt.Fprintf(stderr, "spector: warning: no %s found in %s\n", what, scanDir)
		if what == "routes" {
			// The scan reads the tree below scanDir, so an empty result is
			// usually the wrong framework rather than the wrong directory —
			// and the adapter is guessed from imports, which a root package
			// that imports no router does not have.
			fmt.Fprintf(stderr, "spector: the scan is recursive; try -adapter %s (gin, chi, echo, fiber, gorillamux, httprouter, bunrouter, stdlib) or point -dir at the package that registers routes\n",
				cfg.AdapterName())
		}
	}
	orDir := func(specific string) string {
		if specific == "" {
			return *dirFlag
		}
		return specific
	}

	// -all writes every document a project has, so a project with REST, gRPC
	// and GraphQL is one command rather than three — each with its own flags to
	// get wrong.
	if *all {
		dir := *out
		if dir == "" {
			dir = "."
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail(err)
		}
		emit := func() int {
			type artifact struct {
				file string
				doc  any
				err  error
				// empty reports whether the scan found nothing, which is not a
				// failure: a REST-only project has no protos and should not be
				// handed an empty grpc.json pretending otherwise.
				empty bool
			}

			doc, derr := spector.Generate(cfg)
			gdoc, gerr := spector.GenerateGrpc(cfg)
			qdoc, qerr := spector.GenerateGraphql(cfg)

			// -all writes one file per document; -format yaml renames them
			// and switches the encoding, so the directory is all one format.
			ext := ".json"
			if strings.EqualFold(*format, "yaml") || strings.EqualFold(*format, "yml") {
				ext = ".yaml"
			}
			artifacts := []artifact{
				{"openapi" + ext, doc, derr, doc != nil && len(doc.Paths) == 0},
				{"grpc" + ext, gdoc, gerr, gdoc != nil && len(gdoc.Services) == 0},
				{"graphql" + ext, qdoc, qerr, qdoc != nil && len(qdoc.Queries) == 0 && len(qdoc.Types) == 0},
			}

			written := 0
			for _, a := range artifacts {
				if a.err != nil {
					fmt.Fprintf(stderr, "spector: %s: %v\n", a.file, a.err)
					continue
				}
				if a.empty {
					fmt.Fprintf(stderr, "spector: %s skipped: nothing found in %s\n", a.file, *dirFlag)
					continue
				}
				data, merr := marshalDocument(a.doc, *format, a.file)
				if merr != nil {
					return fail(merr)
				}
				path := filepath.Join(dir, a.file)
				if werr := os.WriteFile(path, data, 0o644); werr != nil {
					return fail(werr)
				}
				fmt.Fprintf(stderr, "wrote %s (%d bytes)\n", path, len(data))
				written++
			}
			if written == 0 {
				fmt.Fprintln(stderr, "spector: nothing was written")
				return 1
			}
			return 0
		}
		if code := emit(); !*watch {
			return code
		}
		return watchLoop(cfg.Dir, stderr, emit)
	}

	// -sdk writes a typed client the caller owns: source to commit and edit,
	// not a runtime to depend on.
	if *sdkLang != "" {
		dir := *sdkOut
		if dir == "" {
			dir = "./sdk"
		}
		emit := func() int {
			opts := spector.SDKOptions{Lang: *sdkLang, Package: *sdkPkg}
			var files []spector.SDKFile
			var gerr error
			if *openapiIn != "" {
				// The client is generated from an existing OpenAPI file rather than
				// by scanning source: hand-written specs and third-party APIs.
				doc, lerr := spector.LoadDocument(*openapiIn)
				if lerr != nil {
					return fail(lerr)
				}
				files, gerr = spector.GenerateSDKFromDocument(doc, opts)
			} else {
				files, gerr = spector.GenerateSDK(cfg, opts)
			}
			if gerr != nil {
				return fail(gerr)
			}
			for _, f := range files {
				path := filepath.Join(dir, f.Name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return fail(err)
				}
				if err := os.WriteFile(path, f.Data, 0o644); err != nil {
					return fail(err)
				}
				fmt.Fprintf(stderr, "wrote %s (%d bytes)\n", path, len(f.Data))
			}
			return 0
		}
		if code := emit(); !*watch {
			return code
		}
		return watchLoop(cfg.Dir, stderr, emit)
	}

	// writeOut sends bytes to -o or stdout, shared by the export modes.
	writeOut := func(data []byte) int {
		if len(data) == 0 || data[len(data)-1] != '\n' {
			data = append(data, '\n')
		}
		if *out == "" {
			if _, werr := stdout.Write(data); werr != nil {
				return fail(werr)
			}
			return 0
		}
		if werr := os.WriteFile(*out, data, 0o644); werr != nil {
			return fail(werr)
		}
		fmt.Fprintln(stderr, "wrote", *out)
		return 0
	}

	// -postman and -markdown are exports of the same document the default mode
	// emits, so they share its generation and only differ in rendering.
	if *postman || *postmanEnv || *markdown || *har || *asyncapi {
		doc, derr := spector.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		if len(doc.Paths) == 0 {
			warnEmpty("routes", *dirFlag)
		}
		if *asyncapi {
			data, aerr := spector.ExportAsyncAPI(doc)
			if aerr != nil {
				return fail(aerr)
			}
			return writeOut(data)
		}
		if *har {
			data, herr := spector.ExportHAR(doc)
			if herr != nil {
				return fail(herr)
			}
			return writeOut(data)
		}
		if *postmanEnv {
			data, perr := spector.ExportPostmanEnvironment(doc)
			if perr != nil {
				return fail(perr)
			}
			return writeOut(data)
		}
		if *postman {
			data, perr := spector.ExportPostman(doc)
			if perr != nil {
				return fail(perr)
			}
			return writeOut(data)
		}
		return writeOut(spector.ExportMarkdown(doc))
	}

	// -gen-tests writes a test file rather than a document. The path is given
	// in full (not a directory) because Go cares that it ends in _test.go.
	if *genTests != "" {
		doc, derr := spector.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		if len(doc.Paths) == 0 {
			warnEmpty("routes", *dirFlag)
		}
		data := spector.GenerateTests(doc, spector.TestgenOptions{Package: *testPkg})
		if !strings.HasSuffix(*genTests, "_test.go") {
			fmt.Fprintln(stderr, "spector: warning: file does not end in _test.go, so `go test` will not run it")
		}
		if err := os.MkdirAll(filepath.Dir(*genTests), 0o755); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(*genTests, data, 0o644); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stderr, "wrote %s (%d bytes)\nrun with: SPECTOR_BASE_URL=http://localhost:8080 go test %s\n",
			*genTests, len(data), filepath.Dir(*genTests))
		return 0
	}

	// -coverage answers "how documented is this?" rather than emitting the
	// document; like -lint its exit code is the result so CI can gate on it.
	if *coverageFlag || *coverageMin > 0 {
		doc, derr := spector.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		report := spector.MeasureCoverage(doc)
		fmt.Fprint(stdout, report.Render())
		if *coverageMin > 0 && report.Percent() < *coverageMin {
			fmt.Fprintf(stderr, "spector: coverage %.1f%% is below the required %.1f%%\n",
				report.Percent(), *coverageMin)
			return 1
		}
		return 0
	}

	// -contract writes artefacts that run against the API, so the document
	// stops being a claim nobody checks.
	if *contractOut != "" {
		pkg := *contractPkg
		if pkg == "" {
			pkg = packageName(*contractOut)
		}
		var formats []string
		for _, f := range strings.Split(*contractFormat, ",") {
			if f = strings.TrimSpace(f); f != "" {
				formats = append(formats, f)
			}
		}

		files, gerr := spector.GenerateContract(cfg, spector.ContractOptions{
			BaseURL: *contractAPI,
			Package: pkg,
			Formats: formats,
		})
		if gerr != nil {
			return fail(gerr)
		}
		if err := os.MkdirAll(*contractOut, 0o755); err != nil {
			return fail(err)
		}
		for _, f := range files {
			path := filepath.Join(*contractOut, f.Name)
			// smoke.sh is meant to be run, not sourced.
			perm := os.FileMode(0o644)
			if strings.HasSuffix(f.Name, ".sh") {
				perm = 0o755
			}
			if err := os.WriteFile(path, f.Data, perm); err != nil {
				return fail(err)
			}
			fmt.Fprintf(stderr, "wrote %s (%d bytes)\n", path, len(f.Data))
		}

		dir := goPackagePath(*contractOut)
		fmt.Fprintf(stderr, "\nspector: run them against a live API with:\n"+
			"  SPECTOR_BASE_URL=http://localhost:8080 go test -tags contract %s\n"+
			"  SPECTOR_BASE_URL=http://localhost:8080 sh %s/smoke.sh\n", dir, dir)
		return 0
	}

	// -evolve compares the current API against a baseline and classifies what
	// changed. Like -lint its exit code is the result, so CI can gate on it.
	if *evolveMode {
		return runEvolve(cfg, evolveConfig{
			since:          *evolveSince,
			baselineDir:    *evolveBaselineDir,
			baselineJSON:   *evolveBaseline,
			format:         *evolveFormat,
			failOnBreaking: *failOnBreaking,
		}, stdout, stderr)
	}

	// -proxy sits in front of a real API and reports where the traffic
	// disagrees with the document. Like -mock it runs until interrupted.
	if *proxyAddr != "" {
		if *proxyMerge && *proxyLearn == "" {
			return fail(fmt.Errorf("-proxy-merge needs -proxy-learn <file> to name the output"))
		}
		doc, derr := spector.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		return runProxy(doc, proxyConfig{
			addr:      *proxyAddr,
			target:    *proxyTarget,
			report:    *proxyReport,
			record:    *proxyRecord,
			recordRaw: *proxyRecordRaw,
			learn:     *proxyLearn,
			merge:     *proxyMerge,
			strict:    *proxyStrict,
			title:     cfg.Title,
			version:   cfg.Version,
		}, stdout, stderr)
	}

	// -mock serves rather than emits, so it does not return while it runs.
	if *mockAddr != "" {
		doc, derr := spector.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		opts := spector.MockOptions{
			AllowCredentials: *mockCreds,
			MaxAge:           *mockMaxAge,
			EnforceAuth:      *mockAuth,
		}
		for _, o := range strings.Split(*mockOrigins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				opts.AllowOrigins = append(opts.AllowOrigins, o)
			}
		}

		fmt.Fprintf(stderr, "spector: mocking %d paths on %s\n", len(doc.Paths), *mockAddr)
		fmt.Fprintln(stderr, "spector: responses are shaped, not stateful — a POST does not change a later GET")
		if len(opts.AllowOrigins) == 0 {
			fmt.Fprintln(stderr, "spector: CORS open to any origin; use -mock-origin to restrict it")
		}
		// Worth saying out loud: this is the combination the CORS spec forbids
		// with a wildcard, so the mock echoes the caller's origin instead.
		if *mockCreds && len(opts.AllowOrigins) == 0 {
			fmt.Fprintln(stderr, "spector: credentials allowed, so the caller's own origin is echoed back rather than *")
		}
		if serr := spector.ServeMock(*mockAddr, doc, opts); serr != nil {
			return fail(serr)
		}
		return 0
	}

	// -lint answers a different question from the other modes: it reports
	// problems rather than emitting a document, and its exit code is the
	// result, so CI can gate on it.
	if *lintOnly {
		routes, serr := spector.ScanRoutes(cfg)
		if serr != nil {
			return fail(serr)
		}
		findings, lerr := spector.Lint(cfg, routes)
		if lerr != nil {
			return fail(lerr)
		}
		for _, f := range findings {
			fmt.Fprintln(stdout, f)
		}
		if len(findings) > 0 {
			fmt.Fprintf(stderr, "spector: %d problem(s) found\n", len(findings))
			return 1
		}
		fmt.Fprintln(stderr, "spector: no routing problems found")
		return 0
	}

	// regen builds the requested document and marshals it. It is a closure
	// rather than straight-line code so -watch can re-run exactly what the
	// first pass did, with the same flags applied. Diagnostics are emitted
	// here, inside the closure, so every regen — the first pass and every
	// -watch re-run — reports dynamic routes, not just the first.
	var routeDiags []spector.Diagnostic
	emitRouteDiags := func() {
		for _, d := range routeDiags {
			fmt.Fprintf(stderr, "spector: %s: dynamic %s, cannot infer path (%s)\n", d.Pos, d.Kind, d.Reason)
		}
		if n := len(routeDiags); n > 0 {
			fmt.Fprintf(stderr, "spector: %d route(s) could not be statically resolved\n", n)
		}
	}
	regen := func() ([]byte, error) {
		var v any
		switch {
		case *grpc:
			gdoc, err := spector.GenerateGrpc(cfg)
			if err != nil {
				return nil, err
			}
			if len(gdoc.Services) == 0 {
				warnEmpty("gRPC services", orDir(*protoDir))
			}
			v = gdoc
		case *gateway:
			gwdoc, err := spector.GenerateGateway(cfg)
			if err != nil {
				return nil, err
			}
			if len(gwdoc.Paths) == 0 {
				warnEmpty("google.api.http annotations", orDir(*protoDir))
			}
			v = gwdoc
		case *graphql:
			qdoc, err := spector.GenerateGraphql(cfg)
			if err != nil {
				return nil, err
			}
			if len(qdoc.Queries) == 0 && len(qdoc.Types) == 0 {
				warnEmpty("GraphQL schema", orDir(*graphqlDir))
			}
			v = qdoc
		default:
			doc, err := spector.Generate(cfg)
			if err != nil {
				return nil, err
			}
			if len(doc.Paths) == 0 {
				warnEmpty("routes", *dirFlag)
			}
			// -merge-learned folds a previously captured observed fragment into the
			// scanned document, filling the routes the AST could not resolve. Done
			// before diagnostics/output so -o, -watch and 3.1 all see the merged doc.
			if *mergeLearned != "" {
				frag, ferr := readDocumentFile(*mergeLearned)
				if ferr != nil {
					return nil, ferr
				}
				doc = spector.MergeObserved(doc, frag)
			}
			routeDiags = doc.Diagnostics
			emitRouteDiags()
			v = doc
			// 3.1 is a conversion of the same document, not a second generator, so
			// everything upstream — adapters, config, middleware — is untouched.
			switch *oasVersion {
			case "", "3.0":
			case "3.1":
				tree, terr := spector.ToV31(doc)
				if terr != nil {
					return nil, terr
				}
				v = tree
			default:
				return nil, fmt.Errorf("unsupported -openapi-version %q (want 3.0 or 3.1)", *oasVersion)
			}
		}

		return marshalDocument(v, *format, *out)
	}

	data, err := regen()
	if err != nil {
		return fail(err)
	}

	if len(routeDiags) > 0 && *strictRoutes {
		return 1
	}

	if *out == "" {
		if _, err := stdout.Write(data); err != nil {
			return fail(err)
		}
		return 0
	}
	if err := os.WriteFile(*out, data, 0644); err != nil {
		return fail(err)
	}
	fmt.Fprintln(stderr, "wrote", *out)
	if *watch {
		return watchLoop(cfg.Dir, stderr, func() int {
			data, merr := regen()
			if merr != nil {
				return fail(merr)
			}
			if werr := os.WriteFile(*out, data, 0644); werr != nil {
				return fail(werr)
			}
			fmt.Fprintln(stderr, "wrote", *out)
			return 0
		})
	}
	return 0
}

// watchInterval is how often the watched tree is re-fingerprinted. A variable
// so tests do not have to wait a wall-clock second per iteration.
var watchInterval = time.Second

// watchMaxIterations bounds the loop for tests; 0 (the default) means forever.
var watchMaxIterations = 0

// watchLoop re-runs emit whenever a source file under dir changes. It polls
// rather than using OS file events: a fingerprint a second is invisible on any
// project, needs no dependency, and behaves identically on every platform.
func watchLoop(dir string, stderr io.Writer, emit func() int) int {
	fmt.Fprintf(stderr, "spector: watching %s for changes (interval %s)\n", dir, watchInterval)
	last := fingerprint(dir)
	for i := 0; watchMaxIterations == 0 || i < watchMaxIterations; i++ {
		time.Sleep(watchInterval)
		cur := fingerprint(dir)
		if cur == last {
			continue
		}
		last = cur
		fmt.Fprintln(stderr, "spector: change detected, regenerating")
		// A failed regeneration does not end the watch: the next save may fix
		// the very error this one introduced.
		emit()
	}
	return 0
}

// fingerprint hashes the name, size and mtime of every source file under dir.
// Content hashing would cost reads for no gain: an edit that changes neither
// size nor mtime does not exist in practice.
func fingerprint(dir string) string {
	h := fnv.New64a()
	// The walk's own callback swallows per-entry errors — a vanished file is
	// the very change the next pass looks for — so there is nothing left for
	// the return value to report.
	_ = filepath.WalkDir(dir, func(path string, d iofs.DirEntry, err error) error {
		if err != nil {
			return nil // a vanished file is itself a change the next pass sees
		}
		if d.IsDir() {
			// Generated output directories would retrigger the watch forever.
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".proto", ".graphql", ".graphqls", ".json":
		default:
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		fmt.Fprintf(h, "%s|%d|%d\n", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return fmt.Sprintf("%x", h.Sum64())
}

// fileConfig is the part of spector.Config a project declares rather than
// derives. It is spelled out here rather than reusing spector.Config directly:
// Config is public API, and giving it JSON tags would turn it into a
// serialization contract that could not be changed afterwards.
type fileConfig struct {
	Title    string                            `json:"title" yaml:"title"`
	Version  string                            `json:"version" yaml:"version"`
	Adapter  string                            `json:"adapter" yaml:"adapter"`
	Servers  []spector.Server                  `json:"servers" yaml:"servers"`
	Security map[string]spector.SecurityScheme `json:"security" yaml:"security"`
	BasePath string                            `json:"basePath" yaml:"basePath"`
	// AccessKey gates the console. It is read here so one file describes the
	// whole deployment, but it has no effect on the document the CLI writes.
	AccessKey string `json:"accessKey" yaml:"accessKey"`
	// Production hides the scanned source from the document and console. A
	// passed -prod flag still wins over the file.
	Production bool `json:"production" yaml:"production"`
	// Routes are hand-declared operations for the routes the AST cannot
	// resolve. They are always taken from the file: there is no flag that
	// could carry them, so nothing on the command line can conflict.
	Routes []spector.ManualRoute `json:"routes" yaml:"routes"`
}

// configNames is the set of filenames applyConfigFile looks for in the scanned
// directory when no -config is given, in order. JSON is first so an existing
// spector.json is never silently overridden by a YAML file beside it.
var configNames = []string{"spector.json", "spector.yaml", "spector.yml"}

// decodeConfig unmarshals config bytes into fc, choosing the format by the
// file's extension: .yaml/.yml is YAML, anything else (including .json and no
// extension) is JSON.
func decodeConfig(path string, data []byte, fc *fileConfig) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return yaml.Unmarshal(data, fc)
	default:
		return json.Unmarshal(data, fc)
	}
}

// applyConfigFile fills cfg from a JSON file, leaving anything the user typed
// on the command line alone. The file is a default, not an override: a flag
// that was actually passed always wins, which cannot be decided by looking at
// values because -version has a non-empty default of its own.
//
// path names a file explicitly and must exist; its format is chosen by
// extension (.yaml/.yml is YAML, otherwise JSON). With no -config, the scanned
// source is checked for spector.json, then spector.yaml, then spector.yml — the
// first found wins — so the console and the CLI agree by default rather than by
// discipline.
func applyConfigFile(cfg *spector.Config, fs *flag.FlagSet, path, dir string) error {
	explicit := path != ""
	var data []byte
	if explicit {
		d, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data = d
	} else {
		// Try each default name in order; the first that exists wins. A directory
		// with no config at all is not an error.
		for _, name := range configNames {
			candidate := filepath.Join(dir, name)
			d, err := os.ReadFile(candidate)
			if err == nil {
				path, data = candidate, d
				break
			}
			if !os.IsNotExist(err) {
				return err
			}
		}
		if data == nil {
			return nil
		}
	}

	var fc fileConfig
	if err := decodeConfig(path, data, &fc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })

	if fc.Title != "" && !passed["title"] {
		cfg.Title = fc.Title
	}
	if fc.Version != "" && !passed["version"] {
		cfg.Version = fc.Version
	}
	if fc.Adapter != "" && !passed["adapter"] {
		cfg.Adapter = fc.Adapter
	}
	cfg.Servers = fc.Servers
	cfg.Security = fc.Security
	cfg.BasePath = fc.BasePath
	cfg.AccessKey = fc.AccessKey
	cfg.Production = fc.Production
	cfg.Routes = fc.Routes
	return nil
}

// readDocumentFile decodes an OpenAPI document (e.g. a -proxy-learn fragment)
// from a JSON file, for -merge-learned. A missing file or bad JSON is fatal and
// named, since the merge cannot proceed without it.
func readDocumentFile(path string) (*spector.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc spector.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &doc, nil
}

// goPackagePath renders an output directory the way `go test` wants it. A
// relative directory needs the ./ prefix to be a package pattern at all, and an
// absolute one has to be left alone: trimming its leading slash would print a
// path that does not exist.
func goPackagePath(dir string) string {
	slashed := filepath.ToSlash(dir)
	if filepath.IsAbs(dir) {
		return slashed
	}
	return "./" + strings.TrimPrefix(strings.TrimPrefix(slashed, "./"), "/")
}

// packageName turns an output directory into a legal Go package name.
func packageName(dir string) string {
	base := filepath.Base(filepath.Clean(dir))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		}
	}
	name := b.String()
	// A package cannot start with a digit, and an empty name is not a package
	// at all; either way "admin" is the honest fallback.
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		return "admin"
	}
	return name
}

// marshalDocument renders the generated document as JSON or YAML. format wins
// when set; otherwise a -o ending in .yaml/.yml means YAML, so "-o api.yaml"
// does not quietly write JSON under a YAML name. Only the document output is
// affected: Postman, HAR and AsyncAPI are formats of their own.
func marshalDocument(v any, format, out string) ([]byte, error) {
	if format == "" {
		switch strings.ToLower(filepath.Ext(out)) {
		case ".yaml", ".yml":
			format = "yaml"
		default:
			format = "json"
		}
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(format) {
	case "json":
		return append(data, '\n'), nil
	case "yaml", "yml":
		return jsonToYAML(data)
	default:
		return nil, fmt.Errorf("unsupported -format %q (want json or yaml)", format)
	}
}

// jsonToYAML re-emits an encoded JSON document as YAML. YAML is a superset of
// JSON, so parsing the JSON into a yaml.Node gives an ordered tree: the output
// keeps the document's own key order instead of the alphabetical order a
// map[string]any round-trip would impose.
func jsonToYAML(data []byte) ([]byte, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	// A JSON document parses as a document node wrapping the real value.
	root := &node
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		root = node.Content[0]
	}
	clearFlowStyle(root)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// clearFlowStyle drops the flow style every node inherits from having been
// parsed out of JSON, so the YAML is emitted as readable block style rather
// than as JSON with a .yaml name.
func clearFlowStyle(n *yaml.Node) {
	if n == nil {
		return
	}
	n.Style = 0
	for _, c := range n.Content {
		clearFlowStyle(c)
	}
}
