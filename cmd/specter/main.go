package main

import (
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

	"github.com/user/specter"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run holds the whole CLI so it can be exercised without a process boundary:
// streams are injected and failures come back as an exit code rather than a
// call to os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specter", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dirFlag := fs.String("dir", ".", "directory to scan")
	configPath := fs.String("config", "", "JSON config file (default: specter.json in -dir, if present)")
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
	sdkLang := fs.String("sdk", "","generate a typed client instead of a document: go, ts, python, js, ruby, php, csharp, rust, kotlin, java")
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
	proxyStrict := fs.Bool("proxy-strict", false, "exit non-zero if any drift was found (for CI)")
	mockAddr := fs.String("mock", "", "serve the document as a mock API on this address (e.g. :8080)")
	mockOrigins := fs.String("mock-origin", "", "comma-separated origins allowed to call the mock (default any)")
	mockCreds := fs.Bool("mock-credentials", false, "allow cookies and Authorization headers on mock requests")
	mockMaxAge := fs.Int("mock-max-age", 0, "seconds a browser may cache the mock's CORS preflight")
	mcpFlag := fs.Bool("mcp", false, "serve specter as an MCP server over stdio")
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
	strictRoutes := fs.Bool("strict-routes", false, "exit non-zero if any route path cannot be statically resolved")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *mcpFlag {
		return runMCP(stderr)
	}

	cfg := specter.Config{
		Dir:        *dirFlag,
		Adapter:    *adapter,
		Title:      *title,
		Version:    *version,
		ProtoDir:   *protoDir,
		GraphqlDir: *graphqlDir,
	}

	fail := func(err error) int {
		fmt.Fprintln(stderr, "specter:", err)
		return 1
	}

	// Servers and security schemes are declared rather than inferred, and a map
	// of schemes does not fit on a command line. Without the file the CLI's
	// document and the console's disagree about the same API.
	if err := applyConfigFile(&cfg, fs, *configPath, *dirFlag); err != nil {
		return fail(err)
	}
	// An empty result is not an error: the scan ran, it just found nothing.
	// A warning names the directory so the cause is obvious.
	warnEmpty := func(what, scanDir string) {
		fmt.Fprintf(stderr, "specter: warning: no %s found in %s\n", what, scanDir)
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

			doc, derr := specter.Generate(cfg)
			gdoc, gerr := specter.GenerateGrpc(cfg)
			qdoc, qerr := specter.GenerateGraphql(cfg)

			artifacts := []artifact{
				{"openapi.json", doc, derr, doc != nil && len(doc.Paths) == 0},
				{"grpc.json", gdoc, gerr, gdoc != nil && len(gdoc.Services) == 0},
				{"graphql.json", qdoc, qerr, qdoc != nil && len(qdoc.Queries) == 0 && len(qdoc.Types) == 0},
			}

			written := 0
			for _, a := range artifacts {
				if a.err != nil {
					fmt.Fprintf(stderr, "specter: %s: %v\n", a.file, a.err)
					continue
				}
				if a.empty {
					fmt.Fprintf(stderr, "specter: %s skipped: nothing found in %s\n", a.file, *dirFlag)
					continue
				}
				data, merr := json.MarshalIndent(a.doc, "", "  ")
				if merr != nil {
					return fail(merr)
				}
				path := filepath.Join(dir, a.file)
				if werr := os.WriteFile(path, append(data, '\n'), 0o644); werr != nil {
					return fail(werr)
				}
				fmt.Fprintf(stderr, "wrote %s (%d bytes)\n", path, len(data)+1)
				written++
			}
			if written == 0 {
				fmt.Fprintln(stderr, "specter: nothing was written")
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
			opts := specter.SDKOptions{Lang: *sdkLang, Package: *sdkPkg}
			var files []specter.SDKFile
			var gerr error
			if *openapiIn != "" {
				// The client is generated from an existing OpenAPI file rather than
				// by scanning source: hand-written specs and third-party APIs.
				doc, lerr := specter.LoadDocument(*openapiIn)
				if lerr != nil {
					return fail(lerr)
				}
				files, gerr = specter.GenerateSDKFromDocument(doc, opts)
			} else {
				files, gerr = specter.GenerateSDK(cfg, opts)
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
		doc, derr := specter.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		if len(doc.Paths) == 0 {
			warnEmpty("routes", *dirFlag)
		}
		if *asyncapi {
			data, aerr := specter.ExportAsyncAPI(doc)
			if aerr != nil {
				return fail(aerr)
			}
			return writeOut(data)
		}
		if *har {
			data, herr := specter.ExportHAR(doc)
			if herr != nil {
				return fail(herr)
			}
			return writeOut(data)
		}
		if *postmanEnv {
			data, perr := specter.ExportPostmanEnvironment(doc)
			if perr != nil {
				return fail(perr)
			}
			return writeOut(data)
		}
		if *postman {
			data, perr := specter.ExportPostman(doc)
			if perr != nil {
				return fail(perr)
			}
			return writeOut(data)
		}
		return writeOut(specter.ExportMarkdown(doc))
	}

	// -gen-tests writes a test file rather than a document. The path is given
	// in full (not a directory) because Go cares that it ends in _test.go.
	if *genTests != "" {
		doc, derr := specter.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		if len(doc.Paths) == 0 {
			warnEmpty("routes", *dirFlag)
		}
		data := specter.GenerateTests(doc, specter.TestgenOptions{Package: *testPkg})
		if !strings.HasSuffix(*genTests, "_test.go") {
			fmt.Fprintln(stderr, "specter: warning: file does not end in _test.go, so `go test` will not run it")
		}
		if err := os.MkdirAll(filepath.Dir(*genTests), 0o755); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(*genTests, data, 0o644); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stderr, "wrote %s (%d bytes)\nrun with: SPECTER_BASE_URL=http://localhost:8080 go test %s\n",
			*genTests, len(data), filepath.Dir(*genTests))
		return 0
	}

	// -coverage answers "how documented is this?" rather than emitting the
	// document; like -lint its exit code is the result so CI can gate on it.
	if *coverageFlag || *coverageMin > 0 {
		doc, derr := specter.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		report := specter.MeasureCoverage(doc)
		fmt.Fprint(stdout, report.Render())
		if *coverageMin > 0 && report.Percent() < *coverageMin {
			fmt.Fprintf(stderr, "specter: coverage %.1f%% is below the required %.1f%%\n",
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

		files, gerr := specter.GenerateContract(cfg, specter.ContractOptions{
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
		fmt.Fprintf(stderr, "\nspecter: run them against a live API with:\n"+
			"  SPECTER_BASE_URL=http://localhost:8080 go test -tags contract %s\n"+
			"  SPECTER_BASE_URL=http://localhost:8080 sh %s/smoke.sh\n", dir, dir)
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
		doc, derr := specter.Generate(cfg)
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
			strict:    *proxyStrict,
			title:     cfg.Title,
			version:   cfg.Version,
		}, stdout, stderr)
	}

	// -mock serves rather than emits, so it does not return while it runs.
	if *mockAddr != "" {
		doc, derr := specter.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		opts := specter.MockOptions{
			AllowCredentials: *mockCreds,
			MaxAge:           *mockMaxAge,
			EnforceAuth:      *mockAuth,
		}
		for _, o := range strings.Split(*mockOrigins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				opts.AllowOrigins = append(opts.AllowOrigins, o)
			}
		}

		fmt.Fprintf(stderr, "specter: mocking %d paths on %s\n", len(doc.Paths), *mockAddr)
		fmt.Fprintln(stderr, "specter: responses are shaped, not stateful — a POST does not change a later GET")
		if len(opts.AllowOrigins) == 0 {
			fmt.Fprintln(stderr, "specter: CORS open to any origin; use -mock-origin to restrict it")
		}
		// Worth saying out loud: this is the combination the CORS spec forbids
		// with a wildcard, so the mock echoes the caller's origin instead.
		if *mockCreds && len(opts.AllowOrigins) == 0 {
			fmt.Fprintln(stderr, "specter: credentials allowed, so the caller's own origin is echoed back rather than *")
		}
		if serr := specter.ServeMock(*mockAddr, doc, opts); serr != nil {
			return fail(serr)
		}
		return 0
	}

	// -lint answers a different question from the other modes: it reports
	// problems rather than emitting a document, and its exit code is the
	// result, so CI can gate on it.
	if *lintOnly {
		routes, serr := specter.ScanRoutes(cfg)
		if serr != nil {
			return fail(serr)
		}
		findings, lerr := specter.Lint(cfg, routes)
		if lerr != nil {
			return fail(lerr)
		}
		for _, f := range findings {
			fmt.Fprintln(stdout, f)
		}
		if len(findings) > 0 {
			fmt.Fprintf(stderr, "specter: %d problem(s) found\n", len(findings))
			return 1
		}
		fmt.Fprintln(stderr, "specter: no routing problems found")
		return 0
	}

	// regen builds the requested document and marshals it. It is a closure
	// rather than straight-line code so -watch can re-run exactly what the
	// first pass did, with the same flags applied.
	var routeDiags []specter.Diagnostic
	regen := func() ([]byte, error) {
		var v any
		switch {
		case *grpc:
			gdoc, err := specter.GenerateGrpc(cfg)
			if err != nil {
				return nil, err
			}
			if len(gdoc.Services) == 0 {
				warnEmpty("gRPC services", orDir(*protoDir))
			}
			v = gdoc
		case *graphql:
			qdoc, err := specter.GenerateGraphql(cfg)
			if err != nil {
				return nil, err
			}
			if len(qdoc.Queries) == 0 && len(qdoc.Types) == 0 {
				warnEmpty("GraphQL schema", orDir(*graphqlDir))
			}
			v = qdoc
		default:
			doc, err := specter.Generate(cfg)
			if err != nil {
				return nil, err
			}
			if len(doc.Paths) == 0 {
				warnEmpty("routes", *dirFlag)
			}
			routeDiags = doc.Diagnostics
			v = doc
			// 3.1 is a conversion of the same document, not a second generator, so
			// everything upstream — adapters, config, middleware — is untouched.
			switch *oasVersion {
			case "", "3.0":
			case "3.1":
				tree, terr := specter.ToV31(doc)
				if terr != nil {
					return nil, terr
				}
				v = tree
			default:
				return nil, fmt.Errorf("unsupported -openapi-version %q (want 3.0 or 3.1)", *oasVersion)
			}
		}

		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	}

	data, err := regen()
	if err != nil {
		return fail(err)
	}

	for _, d := range routeDiags {
		fmt.Fprintf(stderr, "specter: %s: dynamic %s, cannot infer path (%s)\n", d.Pos, d.Kind, d.Reason)
	}
	if n := len(routeDiags); n > 0 {
		fmt.Fprintf(stderr, "specter: %d route(s) could not be statically resolved\n", n)
		if *strictRoutes {
			return 1
		}
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
	fmt.Fprintf(stderr, "specter: watching %s for changes (interval %s)\n", dir, watchInterval)
	last := fingerprint(dir)
	for i := 0; watchMaxIterations == 0 || i < watchMaxIterations; i++ {
		time.Sleep(watchInterval)
		cur := fingerprint(dir)
		if cur == last {
			continue
		}
		last = cur
		fmt.Fprintln(stderr, "specter: change detected, regenerating")
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
	filepath.WalkDir(dir, func(path string, d iofs.DirEntry, err error) error {
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

// fileConfig is the part of specter.Config a project declares rather than
// derives. It is spelled out here rather than reusing specter.Config directly:
// Config is public API, and giving it JSON tags would turn it into a
// serialization contract that could not be changed afterwards.
type fileConfig struct {
	Title    string                            `json:"title"`
	Version  string                            `json:"version"`
	Adapter  string                            `json:"adapter"`
	Servers  []specter.Server                  `json:"servers"`
	Security map[string]specter.SecurityScheme `json:"security"`
	BasePath string                            `json:"basePath"`
	// AccessKey gates the console. It is read here so one file describes the
	// whole deployment, but it has no effect on the document the CLI writes.
	AccessKey string `json:"accessKey"`
}

// applyConfigFile fills cfg from a JSON file, leaving anything the user typed
// on the command line alone. The file is a default, not an override: a flag
// that was actually passed always wins, which cannot be decided by looking at
// values because -version has a non-empty default of its own.
//
// path names a file explicitly and must exist. With no -config, a specter.json
// next to the scanned source is used if there is one, so the console and the
// CLI agree by default rather than by discipline.
func applyConfigFile(cfg *specter.Config, fs *flag.FlagSet, path, dir string) error {
	explicit := path != ""
	if !explicit {
		path = filepath.Join(dir, "specter.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
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
	return nil
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
