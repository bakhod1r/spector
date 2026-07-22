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
	adapter := fs.String("adapter", "", "framework adapter (gin, chi, echo, fiber, gorillamux, stdlib); autodetected if empty")
	title := fs.String("title", "", "API title (defaults to directory name)")
	version := fs.String("version", "0.1.0", "API version")
	out := fs.String("o", "", "output file (defaults to stdout)")
	grpc := fs.Bool("grpc", false, "export the gRPC document (.proto/.pb.go) instead of OpenAPI")
	protoDir := fs.String("proto", "", "directory to scan for gRPC sources (defaults to -dir)")
	graphql := fs.Bool("graphql", false, "export the GraphQL document (.graphql/gqlgen) instead of OpenAPI")
	graphqlDir := fs.String("graphqlDir", "", "directory to scan for GraphQL sources (defaults to -dir)")
	lintOnly := fs.Bool("lint", false, "report routing problems instead of a document; exits 1 if any are found")
	all := fs.Bool("all", false, "write openapi.json, grpc.json and graphql.json into -o (a directory)")
	adminOut := fs.String("admin", "", "generate a gin admin panel into this directory (e.g. ./admin)")
	adminAPI := fs.String("admin-api", "", "base URL the generated panel calls (default: the document's first server)")
	adminPrefix := fs.String("admin-prefix", "/admin", "path the generated panel is served under")
	adminPkg := fs.String("admin-package", "", "package name for the generated panel (default: the directory name)")
	adminImport := fs.String("admin-import", "", "import path of the generated package (default: derived from go.mod)")
	sdkLang := fs.String("sdk", "", "generate a typed client instead of a document: ts or go")
	sdkOut := fs.String("sdk-out", "", "directory the generated client is written into (default ./sdk)")
	sdkPkg := fs.String("sdk-package", "", "package name for the generated Go client (default: client)")
	watch := fs.Bool("watch", false, "stay running and regenerate whenever the scanned sources change")
	mockAddr := fs.String("mock", "", "serve the document as a mock API on this address (e.g. :8080)")
	mockOrigins := fs.String("mock-origin", "", "comma-separated origins allowed to call the mock (default any)")
	mockCreds := fs.Bool("mock-credentials", false, "allow cookies and Authorization headers on mock requests")
	mockMaxAge := fs.Int("mock-max-age", 0, "seconds a browser may cache the mock's CORS preflight")
	mcpFlag := fs.Bool("mcp", false, "serve specter as an MCP server over stdio")
	oasVersion := fs.String("openapi-version", "3.0", "OpenAPI version to emit: 3.0 or 3.1")
	postman := fs.Bool("postman", false, "export a Postman collection v2.1 (Insomnia imports it too)")
	markdown := fs.Bool("markdown", false, "export static Markdown API docs")
	mockAuth := fs.Bool("mock-auth", false, "mock enforces documented security: missing credentials get 401")
	genTests := fs.String("gen-tests", "", "write a Go integration test file to this path (e.g. ./apitest/api_test.go)")
	testPkg := fs.String("test-package", "", "package name for the generated test file (default: apitest)")
	coverageFlag := fs.Bool("coverage", false, "report documentation coverage instead of a document")
	coverageMin := fs.Float64("coverage-min", 0, "exit 1 when coverage is below this percent (implies -coverage)")
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

	// -admin writes a project rather than a document: Go source you own and
	// edit, not a runtime to configure.
	if *adminOut != "" {
		pkg := *adminPkg
		if pkg == "" {
			pkg = packageName(*adminOut)
		}
		imp := *adminImport
		if imp == "" {
			// Derived rather than demanded: the module path plus where the
			// output sits inside it is exactly what the entrypoint needs, and
			// asking for it invites a typo that only shows up at build time.
			imp = importPath(*adminOut)
			if imp == "" {
				fmt.Fprintln(stderr, "specter: no go.mod found, so cmd/adminpanel is skipped; pass -admin-import to generate it")
			}
		}

		files, gerr := specter.GenerateAdmin(cfg, specter.AdminOptions{
			Package:    pkg,
			Prefix:     *adminPrefix,
			BaseURL:    *adminAPI,
			ImportPath: imp,
			Dir:        *adminOut,
		})
		if gerr != nil {
			return fail(gerr)
		}
		for _, f := range files {
			path := filepath.Join(*adminOut, f.Name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fail(err)
			}
			if err := os.WriteFile(path, f.Data, 0o644); err != nil {
				return fail(err)
			}
			fmt.Fprintf(stderr, "wrote %s (%d bytes)\n", path, len(f.Data))
		}
		fmt.Fprintf(stderr, "\nspecter: run it with:\n  go run ./%s/cmd/adminpanel -api http://localhost:8080 -addr :9090\n",
			strings.Trim(filepath.ToSlash(*adminOut), "./"))
		return 0
	}

	// -sdk writes a typed client the caller owns, in the same spirit as -admin:
	// source to commit and edit, not a runtime to depend on.
	if *sdkLang != "" {
		dir := *sdkOut
		if dir == "" {
			dir = "./sdk"
		}
		emit := func() int {
			files, gerr := specter.GenerateSDK(cfg, specter.SDKOptions{
				Lang:    *sdkLang,
				Package: *sdkPkg,
			})
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
	if *postman || *markdown {
		doc, derr := specter.Generate(cfg)
		if derr != nil {
			return fail(derr)
		}
		if len(doc.Paths) == 0 {
			warnEmpty("routes", *dirFlag)
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
	AdminURL string                            `json:"adminUrl"`
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
	cfg.AdminURL = fc.AdminURL
	cfg.AccessKey = fc.AccessKey
	return nil
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

// importPath derives the generated package's import path from the nearest
// go.mod and where the output directory sits beneath it. It returns "" when
// there is no module to derive from, which the caller reports rather than
// guessing a path that would not compile.
func importPath(out string) string {
	abs, err := filepath.Abs(out)
	if err != nil {
		return ""
	}
	dir := abs
	for {
		data, rerr := os.ReadFile(filepath.Join(dir, "go.mod"))
		if rerr == nil {
			module := moduleOf(data)
			if module == "" {
				return ""
			}
			rel, rerr := filepath.Rel(dir, abs)
			if rerr != nil || rel == "." {
				return module
			}
			// An output outside the module cannot be imported from it.
			if strings.HasPrefix(rel, "..") {
				return ""
			}
			return module + "/" + filepath.ToSlash(rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func moduleOf(gomod []byte) string {
	for _, line := range strings.Split(string(gomod), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module"); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}
