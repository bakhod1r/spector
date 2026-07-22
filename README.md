# Specter

<img src="docs/assets/specter.png" alt="Specter" width="420">

Specter generates OpenAPI 3.0 documents and a browsable API console straight
from your Go source — no annotations, no code generation step, no runtime
reflection. It reads your routing code and handlers as an AST and infers paths,
parameters, request/response types, and status codes. It also documents gRPC
services from `.proto` files or generated `*.pb.go` stubs, and GraphQL schemas
from `.graphql` SDL or gqlgen-generated Go code.

```
go install github.com/user/specter/cmd/specter@latest
```

## Quick start (CLI)

```sh
# Every document a project has, in one command
specter -all -dir . -o ./specs

# OpenAPI from the current package
specter -dir ./api -title "Users API" -version 1.0.0 -o openapi.json

# gRPC document from .proto or generated *.pb.go
specter -grpc -dir ./proto -o grpc.json

# GraphQL document from .graphql SDL or gqlgen-generated Go
specter -graphql -dir ./graph -o graphql.json
```

| Flag          | Description                                                |
| ------------- | ---------------------------------------------------------- |
| `-dir`        | Directory to scan (default `.`)                            |
| `-config`     | JSON config file (default: `specter.json` in `-dir`, if present) |
| `-adapter`    | `gin`, `chi`, `echo`, `fiber`, `gorillamux`, or `stdlib`; autodetected when empty |
| `-title`      | API title (defaults to the directory name)                 |
| `-version`    | API version (default `0.1.0`)                              |
| `-grpc`       | Export the gRPC document instead of OpenAPI                |
| `-proto`      | Directory to scan for gRPC sources (autodetected when empty) |
| `-graphql`    | Export the GraphQL document instead of OpenAPI             |
| `-graphqlDir` | Directory to scan for GraphQL sources (autodetected when empty) |
| `-o`          | Output file (defaults to stdout)                           |
| `-all`        | Write openapi.json, grpc.json and graphql.json into `-o` (a directory) |
| `-lint`       | Report routing problems instead of a document; exits 1 if any |
| `-mock`       | Serve the document as a mock API on an address, e.g. `:8080` |
| `-mock-origin` | Comma-separated origins allowed to call the mock (default any) |
| `-mock-credentials` | Allow cookies and `Authorization` headers on mock requests |
| `-mock-max-age` | Seconds a browser may cache the mock's CORS preflight |
| `-admin`      | Generate a gin admin panel into this directory, e.g. `./admin` |
| `-admin-api`  | Base URL the generated panel calls (default: the document's first server) |
| `-admin-prefix` | Path the panel is served under (default `/admin`)        |
| `-admin-package` | Package name for the generated panel (default: the directory name) |
| `-admin-import` | Import path of the generated package (derived from `go.mod`) |
| `-sdk`        | Generate a typed client instead of a document: `ts` or `go` |
| `-sdk-out`    | Directory the client is written into (default `./sdk`)     |
| `-sdk-package` | Package name for the generated Go client (default `client`) |
| `-watch`      | Stay running and regenerate whenever the scanned sources change |

### Client SDKs

```sh
specter -dir ./api -sdk ts -sdk-out ./web/src/api
specter -dir ./api -sdk go -sdk-package usersapi -sdk-out ./client
```

One file, no runtime dependency: the TypeScript client is `fetch`, the Go client
is `net/http`. Each schema becomes an `interface`/`struct` and each operation a
typed method, named after its `operationId` when the document has one and after
its method and path otherwise.

```ts
const api = new Client({ baseUrl: "https://api.example.com", token });
const users = await api.listUsers({ limit: 20 }); // User[]
```

```go
api := usersapi.New("https://api.example.com")
api.Token = token
users, err := api.ListUsers(ctx, nil) // []User
```

The output is source you own, in the same spirit as the admin panel: commit it,
edit it, and regenerate when the API changes. It is not a framework to
configure, and nothing imports specter at runtime.

### Watch mode

`-watch` keeps the command running and regenerates when a source file changes.
It combines with the modes that write files — `-o`, `-all`, `-sdk` — so a client
or a spec stays in step with the code while you edit it:

```sh
specter -dir ./api -o openapi.json -watch
specter -dir ./api -sdk ts -sdk-out ./web/src/api -watch
```

The tree is polled once a second and fingerprinted by name, size and mtime;
`.git`, `vendor` and `node_modules` are skipped. A regeneration that fails does
not end the watch — the next save may be the fix.

### `specter.json`

Servers and security schemes are declared, not inferred, and a map of schemes
does not fit on a command line. Put them in a `specter.json` next to the code
and the CLI writes the same document the embedded console serves:

```json
{
  "title": "Shop API",
  "version": "1.2.0",
  "servers": [{ "url": "https://api.example.com", "description": "production" }],
  "security": {
    "bearerAuth": { "type": "http", "scheme": "bearer", "bearerFormat": "JWT" }
  },
  "basePath": "/docs",
  "adminUrl": "/admin",
  "accessKey": ""
}
```

It is picked up automatically when it sits in `-dir`; `-config` names one
elsewhere. The file is a default, not an override — a flag you actually typed
wins. A `-config` that does not exist, or a file that does not parse, is an
error rather than a silent fallback.

## Embedded console (library)

Specter is meant to be added to a service you already have: import it, mount
it on the router you already built, and the console documents whatever that
service serves. There is no build step and nothing to keep in sync — the
document is derived from the source at startup.

```go
import (
    "github.com/user/specter"
    "github.com/user/specter/mount"
)

func main() {
    r := gin.Default()
    registerYourRoutes(r)          // the service you already have

    mount.Gin(r, specter.Config{
        Dir:       ".",            // where to read the routing code
        Title:     "Users API",
        Version:   "1.0.0",
        BasePath:  "/docs",        // where to mount it; "/docs" is the default
        AccessKey: cfg.SpecterKey, // your app decides where this comes from
    })

    r.Run(":8080")                 // one server, yours
}
```

**Specter never listens on anything.** The `mount` functions register routes on the router
you pass it, and `specter.Handler` is a plain `http.Handler` — there is no
second port to open, no goroutine started, and no separate process. The console
is served by your server, behind your middleware and your TLS, and it goes away
when your server does.

With the mount above:

```
GET  /docs/                -> HTML console
GET  /docs/openapi.json    -> OpenAPI 3.0 document
GET  /docs/grpc.json       -> gRPC document
GET  /docs/graphql.json    -> GraphQL document
POST /docs/grpc/invoke     -> gRPC call proxy
```

Set `BasePath` to move the whole set: `BasePath: "/internal/api-docs"` serves
the console at `/internal/api-docs/` and the spec at
`/internal/api-docs/openapi.json`.

### Other routers

`mount` has one function per framework, all with the same signature and the
same behaviour:

```go
mount.Gin(r, cfg)      // gin.IRouter
mount.Echo(e, cfg)     // *echo.Echo
mount.Chi(r, cfg)      // chi.Router
mount.Stdlib(mux, cfg) // *http.ServeMux
mount.Fiber(app, cfg)  // fiber.Router
mount.GorillaMux(r, cfg) // *mux.Router
```

Importing `mount` compiles all six frameworks into your binary. If that
matters, skip the package: `specter.Handler(cfg)` is a plain `http.Handler`,
and mounting it by hand is two lines.

```go
mux.Handle("/docs/", http.StripPrefix("/docs", specter.Handler(cfg)))
```

The root `specter` package does not import `mount`, so this path costs no
framework dependencies at all.

Fiber is the one with a caveat: it runs on fasthttp, so each request crosses an
adaptor that rebuilds it as an `*http.Request`. Fine for a console, worth
knowing before you put it on a hot path.

Everything Specter needs arrives through `Config`. The library reads no
environment variables and no config files of its own, so where a value comes
from — env, a secret manager, a flag, a config struct — stays your
application's decision.

## Source links

Specter reads the AST, so it knows the file and line every operation came from.
Each operation carries it as a vendor extension:

```json
"get": {
  "operationId": "listCarts",
  "x-specter-source": { "file": "main.go", "line": 463 }
}
```

In the console each operation has a **View source** button that shows the
handler with the registering line highlighted. The code is fetched on demand
from `GET <base>/source?file=…&line=…` rather than embedded in the spec, which
would inflate a document served to every visitor for a panel most readers never
open.

`file` is relative to the scanned directory. Absolute paths would differ per
machine and leak the developer's home directory into a committed artifact.

This is the one endpoint that reads files for a request, so it is deliberately
narrow: `.go` files only, resolved and then checked to be inside the scanned
directory — symlinks included, since a "reject `..`" filter does not see them —
and it sits behind `AccessKey` like everything else. Failures answer 404 without
saying why, so a caller cannot use the response to map what exists outside the
tree.

The snippet comes from the running server's filesystem. A binary deployed
without its sources still documents fine; the panel just reports that the code
is not available there.

## Validation constraints

The struct scanner reads `binding:"..."` (gin) and `validate:"..."`
(go-playground/validator) alongside `json:"..."`, and turns the rules that have
a JSON Schema equivalent into real constraints:

```go
type CreateCartReq struct {
    UserID int        `json:"userId" binding:"required,gte=1"`
    Email  string     `json:"email"  binding:"required,email"`
    Note   string     `json:"note"   binding:"max=200"`
    Tier   string     `json:"tier"   binding:"oneof=free pro enterprise"`
    Items  []LineItem `json:"items"  binding:"required,min=1,max=50"`
}
```

```json
"userId": { "type": "integer", "minimum": 1 },
"email":  { "type": "string", "format": "email" },
"note":   { "type": "string", "maxLength": 200 },
"tier":   { "type": "string", "enum": ["free", "pro", "enterprise"] },
"items":  { "type": "array", "minItems": 1, "maxItems": 50 },
"required": ["userId", "email", "items"]
```

`min` and `max` mean three different things depending on the field: a value
bound on a number, a length on a string, a count on an array. That is why
constraints are applied after the type is resolved.

Rules with no JSON Schema equivalent — `gtfield`, `required_with`, `contains`,
custom validators — are ignored in silence, and so is a malformed tag. A typo in
a struct tag is not a reason to stop documenting an API.

Specter does not validate anything at runtime. It never sits in the request
path; it only reads the source.

## Standards advice

Specter reviews the generated document against the HTTP and JSON standards and
reports where an API diverges, in the console next to each operation:

```json
"x-specter-advice": [{
  "rule": "rfc9457-content-type",
  "severity": "should",
  "message": "404 returns application/json; error responses should use application/problem+json …",
  "reference": "RFC 9457 (Problem Details for HTTP APIs)"
}]
```

Current rules:

| Rule | Standard | What it says |
| ---- | -------- | ------------ |
| `rfc9457-content-type` | RFC 9457 | Error bodies should be `application/problem+json` |
| `rfc9457-fields` | RFC 9457 | A near-problem document is missing `type`/`instance`/… |
| `no-error-response` | RFC 9110 | No failure response is documented at all |
| `post-created` | RFC 9110 | A creating POST should answer 201 with `Location` |
| `delete-no-content` | RFC 9110 | An empty 200 on DELETE is better as 204 |

**These are recommendations, never rewrites.** Specter documents what the code
does. Reshaping an error body in the document to match RFC 9457 would make it
describe an aspiration instead of a service, so the advice is attached and the
decision stays yours.

Every entry cites the standard it comes from — a recommendation without a
citation is just an assertion — and an API that already conforms is left alone,
because a linter that fires on correct code teaches people to ignore it.

## Dependency map

Reading the AST also shows what a handler reaches: a database, another service,
a cache, a queue. Each operation carries what was found, and the console shows
it as a row of chips.

```json
"x-specter-calls": [
  { "kind": "db",    "target": "db.ExecContext",      "confidence": "likely" },
  { "kind": "http",  "target": "http.Post",           "confidence": "certain" },
  { "kind": "queue", "target": "writer.WriteMessages","confidence": "likely" }
]
```

Handlers usually delegate, so calls are followed up to three levels down —
handler → service → repository — which is where the query normally lives.

**Read the confidence.** Specter has no type checker, so there are two ways a
call gets identified, and they are not equally trustworthy:

- `certain` — the call went through an imported package (`http.Post`,
  `sql.Open`). The import statement proves what it is.
- `likely` — it was matched on the receiver's name (`db.Query`, `cache.Get`).
  That is a convention, and conventions are sometimes wrong.

The console draws `likely` chips with a dashed border and says why. A
dependency map that presents guesses as facts is worse than no map, because it
gets believed.

Calls that cannot be classified are not reported. Listing every method call in a
handler would bury the few that matter, and a handler that reaches nothing shows
nothing — silence has to be reportable for the map to mean anything.

`examples/deps` is a small service with real dependencies; `examples/shop` is
in-memory on purpose and correctly shows none.

## Mock server

A frontend does not have to wait for the backend:

```sh
specter -mock :8080 -dir ./api
```

Every documented path answers with a body that satisfies its own response
schema — enums, formats, bounds and lengths included, because a mock that
returns data its own document would reject is worse than none: the client
passes locally and fails against the real API, which is the exact failure a
mock exists to prevent.

```sh
curl localhost:8080/api/v1/carts/42
# { "id": 42, "items": [...], "subtotal": { "amount": 1.5, ... } }
```

Path parameters are echoed back, so `GET /users/42` answers with id 42 rather
than a fabricated one — the only realism available without inventing state.

Force any documented status to exercise error handling:

```sh
curl "localhost:8080/api/v1/carts/42?__status=404"
```

An undocumented status is refused rather than ignored, so a client cannot
believe it tested a path the API cannot produce.

**The mock is a separate process on its own port**, and it is not mountable on
your router. That is deliberate: the point at which a mock is useful is the
point at which the backend does not exist yet, so there is no router to mount
it on. It also keeps code that fabricates responses out of the request path of
a real service entirely — no flag, no header, and no configuration that could
turn a production route into a source of invented data.

### CORS

A separate port means a separate origin, so every browser call to the mock is a
cross-origin request. By default it is open to anyone, which is right for a mock
whose caller runs on whatever port the dev server picked today:

```sh
specter -mock :8080 -dir ./api
```

Restrict it, or allow credentials, when the default does not fit:

```sh
specter -mock :8080 -dir ./api \
  -mock-origin http://localhost:5173,http://localhost:3000 \
  -mock-credentials
```

**Credentials change how the origin is answered, and that is not optional.** The
CORS specification forbids pairing `Access-Control-Allow-Origin: *` with
credentials — browsers reject it outright — so with `-mock-credentials` the
caller's own origin is echoed back instead of a wildcard. That is safe here and
only here: the mock serves fabricated data and has no session behind it. The
same trick on a real API would be a vulnerability.

An origin that is not allowed receives no CORS headers at all, which is what
makes the browser block it. The request is still answered, because CORS is
enforced by the browser and refusing it server-side would give a misleading
picture of how the real API behaves.

`Vary: Origin` is sent whenever the response depends on the caller's origin, so
a cache cannot hand one origin's response to another.

As a library:

```go
doc, _ := specter.Generate(cfg)
specter.ServeMock(":8080", doc, specter.MockOptions{
    AllowOrigins:     []string{"http://localhost:5173"},
    AllowCredentials: true,
    MaxAge:           600,
})
```

**It is shape, not state.** Two GETs return the same body, and a POST does not
change what a later GET returns. Making it stateful would mean guessing at
semantics the document does not describe, and a mock that is subtly wrong about
behaviour is worse than one that is obviously only about shape.

## Admin panel

Generate a working admin panel from the scanned API:

```sh
specter -admin ./admin -dir .
go run ./admin/cmd/adminpanel -api http://localhost:8080 -addr :9090
```

It runs on its own router and its own port. The panel is an HTTP client of your
API — it never touches your database, and nothing in your service changes to run
it. Mount it into an existing router instead if you prefer:

```go
admin.Mount(r, admin.Config{BaseURL: "http://localhost:8080", Prefix: "/admin"})
```

Each resource gets a master table, a read-only detail view, and a per-row menu.

**The menu is derived, never assumed.** Entries exist only where the endpoint
does:

| Endpoint found            | What appears        |
|---------------------------|---------------------|
| `GET /users`              | the resource itself |
| `GET /users/{id}`         | row click, "View"   |
| `POST /users`             | the "New" button    |
| `PUT` / `PATCH /users/{id}` | "Edit"            |
| `DELETE /users/{id}`      | "Delete"            |

A read-only API generates a read-only screen. A button that exists and does
nothing is worse than no button, so none is rendered.

**Labels come from your handler names.** A `POST /carts` whose handler is
`openCart` gets a button reading "Open Cart", not a generic "Create" — that is
the name your team chose. Collection titles come from the path instead, since
`/categories` is spelled correctly where `listCategorys` is not.

Columns come from the list response schema, forms from the request body schema:
`binding:"required"` marks a field required, `oneof` becomes a `<select>`, and
nested objects are edited as JSON rather than silently flattened.

A collection with no `GET` list endpoint is not made into a resource, and
WebSocket/SSE endpoints are excluded — neither has a table to fill.

### What it does not know

An OpenAPI document carries paths, schemas and security. It does not carry
which column matters, which field is a secret, what a status value means, or how
two resources relate. That is why `-admin` emits **source rather than a
runtime**: `resources.go` is regenerated and marked `DO NOT EDIT`, while
`admin.go` and `templates/` are yours to change and are never overwritten by
intent.

If the API is guarded, pass the credential through:

```sh
go run ./admin/cmd/adminpanel -header "Authorization:Bearer $TOKEN"
```

The panel reads required headers off the spec, so it warns at startup when the
API is guarded and no header was supplied.

## Linting routes

Three routing mistakes compile cleanly, start cleanly, and fail silently:

```sh
specter -lint -dir ./api
```

```
main.go:88:  duplicate-route: GET /users is registered more than once; one registration will never run
main.go:142: orphan-handler: deleteUser looks like a handler but no route registers it
main.go:97:  shadowed-route: GET /users/me may be shadowed by /users/{id} registered earlier at main.go:94
```

- **orphan-handler** — a function shaped like a handler that no route
  registers. Usually a renamed function or a deleted registration: the code
  still builds and the endpoint quietly stops existing.
- **duplicate-route** — the same method and path registered twice. One of them
  never runs.
- **shadowed-route** — a literal path registered after a parameterised one that
  matches it, so `/users/me` may be answered by the `/users/{id}` handler.

It exits 1 when it finds anything, so CI can gate on it:

```yaml
- run: go run github.com/user/specter/cmd/specter -lint -dir ./api
```

Handlers are recognised by signature rather than by name, so ordinary helpers
are not flagged. Shadowing is reported for every framework, not only the ones
that resolve it by registration order — a reader of the code cannot tell which
handler serves `/users/me` without knowing the router's matching rules, and
that ambiguity is worth removing either way.

## Supported REST frameworks

| Framework      | Routes | Path params | Query | Header | Groups / versioning | Status codes | Middleware |
| -------------- | :----: | :---------: | :---: | :----: | :-----------------: | :----------: | :--------: |
| gin            |   ✅   |     ✅      |  ✅   |   ✅   |   `r.Group(...)`    |      ✅      |     ✅     |
| chi            |   ✅   |     ✅      |  ✅   |   ✅   |   `r.Route(...)`    |      ✅      |     ✅     |
| echo           |   ✅   |     ✅      |  ✅   |   ✅   |   `e.Group(...)`    |      ✅      |     ✅     |
| fiber          |   ✅   |     ✅      |  ✅   |   ✅   |  `app.Group(...)`   |      ✅      |     ✅     |
| gorilla/mux    |   ✅   |     ✅      |  ✅   |   ✅   | `PathPrefix(...).Subrouter()` | ✅ |    ✅     |
| net/http (1.22)|   ✅   |     ✅      |  ✅   |   ✅   | sub-mux + `StripPrefix` | ✅      |     ✅     |

What Specter infers from handlers:

- **Request/response bodies** from `c.ShouldBindJSON`, `c.Bind`, `c.BodyParser`,
  `c.JSON`, `json.Decoder/Encoder`, `render.JSON`, etc., resolved to `$ref`
  schemas.
- **Query & header parameters** from `c.Query`, `c.QueryParam`, `c.GetHeader`,
  `r.URL.Query().Get`, `r.Header.Get`, `r.FormValue`.
- **Real status codes** from `c.JSON(201, ...)`, `w.WriteHeader(http.StatusNotFound)`,
  `c.Status(...)`, `c.NoContent(...)`, `c.SendStatus(...)`, and fiber's
  `c.Status(201).JSON(...)` chain — multiple responses per operation are
  supported, and the primary response type is taken from the first 2xx rather
  than whichever body the handler happened to write first.
- **Summaries & descriptions** from the handler's Go doc comment.

Struct schemas support enums (`type Status string` + typed consts), embedded
structs (composed via `allOf`), `time.Time`, maps, and slices.

## gRPC

Specter documents gRPC services two ways:

- **`.proto` sources** — services, methods, streaming, messages, and enum
  variant names.
- **Generated `*.pb.go` stubs** — reconstructed from `grpc.ServiceDesc` values
  and the server interfaces when the original protos are not available.

The console can invoke unary and server-streaming RPCs against a running target
(via server reflection or the local protos).

## GraphQL

Specter documents GraphQL schemas two ways:

- **`.graphql` / `.graphqls` SDL** — object, input, interface and enum types
  plus the fields on the `Query`, `Mutation`, and `Subscription` root types,
  with argument types and doc-string descriptions.
- **gqlgen-generated Go** — reconstructed from the `QueryResolver` /
  `MutationResolver` / `SubscriptionResolver` interfaces and the generated
  model structs when the original schema files are not available.

The console shows a GraphQL tab listing each root field with its arguments,
return type, and the referenced types.

## Servers and security

Which hosts serve the API cannot be read from source, and the exact shape of a
security scheme — where the token goes, what format it is in — is a detail no
middleware name reveals. Declare them:

```go
specter.Config{
    Dir: ".",
    Servers: []specter.Server{
        {URL: "https://api.example.com", Description: "production"},
        {URL: "http://localhost:8080"},
    },
    Security: map[string]specter.SecurityScheme{
        "bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
        "apiKeyAuth": {Type: "apiKey", Name: "X-API-Key", In: "header"},
    },
}
```

These land in `servers`, `components.securitySchemes`, and a document-level
`security` block. Multiple schemes are listed as alternatives — any one
satisfies a request. A declared scheme always beats an inferred one.

Schemes are emitted in name order, so regenerating produces a byte-identical
document rather than churn in review.

### Which routes are protected

That part *is* read from source. Authentication almost never appears in the
handler — it runs in middleware on the router — so a generator that reads only
handler bodies documents every endpoint as public, including the ones that
answer 401 to everybody.

Specter follows the middleware instead, per route and in order:

| Router     | How middleware is found                                        |
| ---------- | -------------------------------------------------------------- |
| gin        | `r.Use(...)`, `r.Group(path, mw...)`, `r.GET(path, mw..., h)`   |
| echo       | `e.Use(...)`, `e.Group(path, mw...)`, `e.GET(path, h, mw...)`   |
| chi        | `r.Use(...)`, `r.Route`/`r.Group` closures, `r.With(mw).Get(...)` |
| net/http   | wrapping: `mw(handler)`, a wrapped mounted sub-mux, and the wrapper around the server's own handler |

Position decides: `r.Use(x)` applies to what is registered after it, and a
guard on one group never reaches its siblings.

The middleware's *name* is a convention, so what it produces is reported as a
guess — `JWTAuth`, `RequireAPIKey`, `CORS`, `RateLimiter` are recognised. Its
*body* is evidence, and overrides the guess: the headers it reads become
required parameters, and the statuses it rejects with (`c.AbortWithStatus`,
`http.Error`, `echo.NewHTTPError`, a `WriteHeader` followed by `return`) become
documented responses. That is what makes a `TenantGuard` or `SignMiddleware` —
names no pattern list could know — documentable at all.

## Gating the console

By default the console is served to anyone who can reach the route. Set an
access key to require a shared secret:

```go
mount.Gin(r, specter.Config{
    Dir:       ".",
    AccessKey: cfg.SpecterKey,   // empty = open, the default
})
```

The key is a plain `Config` field: Specter never reads it from the environment
itself, so it fits whatever your service already uses for secrets.

Open it once with the key in the URL — `/docs/?key=<value>` — and the key is
stored in an `HttpOnly` cookie so the page's own requests carry it. Scripts and
CI can send `X-Specter-Key` instead. Without a valid key every route under the
handler answers 404, including `grpc/invoke`.

**This is a deployment gate, not authentication.** There are no accounts, no
expiry, and no revocation short of changing the value and restarting. Everyone
who holds the key has full access, and the console can invoke your gRPC
methods, so treat it the way you would treat a database password: keep it out
of source, and put a real authenticating proxy in front of anything that needs
per-user access.

## Realtime

The console's Realtime tab connects to three transports from the browser. It
is a client only — Specter does not infer these endpoints from your code, you
type the URL.

- **WebSocket** — connect, watch inbound frames, send payloads.
- **SSE** — `EventSource`. It is GET-only and cannot carry custom headers, so
  auth has to ride in the query string. Named events reach only matching
  listeners, so the pane asks which names to subscribe to.
- **MQTT** — over `ws://`, using a small hand-written MQTT 3.1.1 codec, since
  Specter ships as one file with no external assets. Browsers cannot open raw
  TCP, so the broker needs a WebSocket listener (Mosquitto: `listener 9001` +
  `protocol websockets`).

`examples/shop` serves `/events` and `/ws` so the tab has something to talk to.

## Architecture

```
specter.go            public API: Generate, GenerateGrpc, GenerateGraphql, Handler
cmd/specter           CLI
internal/core         OpenAPI/gRPC/GraphQL model + struct→schema scanner
internal/adapter/*    gin, chi, echo, fiber, gorillamux, stdlib route scanners
                      (shared handler analysis in astutil)
internal/gen          routes + schemas -> OpenAPI document
internal/sdk          OpenAPI document -> TypeScript / Go client source
internal/proto        .proto  -> gRPC document
internal/pbgo         *.pb.go -> gRPC document
internal/graphqlsdl   .graphql -> GraphQL document
internal/gqlgenx      gqlgen Go code -> GraphQL document
internal/grpcx        gRPC invoke proxy (grpcurl)
internal/ui           embedded HTML console (single file, no assets)
mount                 gin/echo/chi/stdlib/fiber/gorillamux mount helpers
```

The console's realtime clients (WebSocket, SSE, and the MQTT codec) live in
`internal/ui/ui.html` alongside the rest of the page.

## Testing

```sh
go test ./...          # unit + integration
go test -race ./...    # what CI runs
```

The console's stateful behaviour — export/import, Execute, routing, and the
realtime panes — cannot be reached from Go tests, so it has a browser suite:

```sh
cd e2e
npm install && npx playwright install chromium
mosquitto -c mosquitto.conf -d    # optional; the MQTT suite skips without it
npm test
```

`npm test` builds and starts `examples/shop` itself on a free port. That is
deliberate: pointing the suite at an already-running server makes it easy to
test a stale binary and believe the result.

The MQTT pane is checked against Mosquitto rather than a stub. The client
codec is hand-written, so an independent broker is the only thing that
catches a misreading of the protocol.

## Limitations

- REST inference is AST-based; dynamically registered routes or params built
  from non-literal values are not detected.
- net/http grouping is limited to the sub-mux + `http.StripPrefix` idiom
  (the standard mux has no native groups).
- gRPC client-streaming and bidirectional RPCs are documented but not
  interactively invocable from the console.
- `.pb.go` enums surface as integers; `.proto` enums surface their names.
- The Go (gqlgen) GraphQL fallback reports Go type names and loses
  non-null/enum detail, since the SDL is not present to map them. Reading the
  `.graphql` SDL directly keeps that detail.
