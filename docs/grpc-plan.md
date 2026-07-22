# Specter — Proto/gRPC qo'llab-quvvatlash + tab-view UI rejasi

## Maqsad

Hozir Specter faqat REST (gin/chi/stdlib AST). Qo'shiladigan:
1. `.proto` fayllarni o'qib, gRPC **service**, **rpc method**, **message** larni ajratish.
2. Ularni UI'da alohida **gRPC** tab ostida ko'rsatish (REST | gRPC tab-view).
3. Har method uchun input/output message schema, streaming turi, va **grpcurl**
   buyrug'ini generatsiya (REST'dagi "Copy as cURL" ekvivalenti).

Uslub o'zgarmaydi: manba fayllarni parse qilib metama'lumot chiqarish,
server minimal, UI bitta self-contained fayl.

## Kutubxona tanlovi

`github.com/emicklei/proto` — sof Go proto3 parser (protoc/compiler kerak emas,
AST darajasida o'qiydi). Specter falsafasiga mos (AST scan). `go.mod`ga qo'shiladi.
(Muqobil: `jhump/protoreflect/desc/protoparse` — kuchliroq, lekin og'irroq;
importlarni resolve qilishi kerak. Boshda emicklei yetarli.)

## Core model (yangi tur — REST'ga tegmaydi)

```
GrpcDoc {
  Package  string
  Services []GrpcService
  Messages map[string]*core.Schema   // message -> Schema (mavjud tur qayta ishlatiladi)
}

GrpcService {
  Name    string          // "UserService"
  FullName string         // "shop.v1.UserService"  (package + name)
  Methods []GrpcMethod
}

GrpcMethod {
  Name       string       // "GetUser"
  InputType  string       // message nomi -> Messages'da $ref
  OutputType string
  ClientStreaming bool
  ServerStreaming bool     // ikkovi: unary / server-stream / client-stream / bidi
}
```

Message -> `core.Schema` map (mavjud `Schema` bilan bir xil, UI qayta ishlatadi):
- skalar: `string`->string, `int32/int64/uint../sint../fixed..`->integer,
  `float/double`->number, `bool`->boolean, `bytes`->string(byte)
- `repeated T` -> `{type:array, items: T}`
- `map<K,V>` -> `{type:object, additionalProperties: V}`
- nested/message tur -> `$ref: #/components/schemas/<Message>`
- `enum` -> `{type:string, enum:[...]}` (yoki integer; boshda string nomlar)
- `oneof` -> hozircha har variant alohida property (soddalashtirilgan)

## Parser (pseudocode) — `internal/proto/proto.go`

```
function ScanProto(dir) -> (GrpcDoc, error):
    files = glob(dir, "**/*.proto")
    doc = GrpcDoc{ Messages: {} }
    for f in files:
        ast = protoparser.Parse(f)          // emicklei/proto
        pkg = ast.package
        walk(ast):
            on Message m:  doc.Messages[m.name] = messageToSchema(m)
            on Enum e:     doc.Messages[e.name] = enumToSchema(e)
            on Service s:
                svc = GrpcService{ Name:s.name, FullName: pkg+"."+s.name }
                for rpc in s.rpcs:
                    svc.Methods.add(GrpcMethod{
                        Name: rpc.name,
                        InputType: rpc.requestType, OutputType: rpc.returnsType,
                        ClientStreaming: rpc.streamsRequest,
                        ServerStreaming: rpc.streamsReturns })
                doc.Services.add(svc)
    return doc

function messageToSchema(m):
    s = Schema{ type:"object", properties:{} }
    for field in m.fields:
        s.properties[field.jsonName] = fieldToSchema(field)   // scalar/repeated/map/ref
    return s
```

Faqat ishlatilgan message'larni chiqarish (REST'dagi `collect` singari):
service input/output'lardan boshlab `$ref` grafini kuzatib yopiq to'plam.

## Server / kutubxona API o'zgarishi

- `specter.Config`ga: `ProtoDir string` (bo'sh bo'lsa `Dir`dan `*.proto` qidiriladi).
- `Handler`:
  - `/openapi.json` — avvalgidek REST.
  - `/grpc.json` — `GrpcDoc` (proto topilsa; aks holda `{services:[]}`).
- Lazy generatsiya, `sync.Once`; ikkalasi mustaqil.

## UI — tab-view (pseudocode)

```
Topbar: [Env] [Manage] [Search]      Tabs: ( REST | gRPC )

on tab REST  -> hozirgi ko'rinish (kategoriyalar, opCard, models)
on tab gRPC  -> fetch("grpc.json"); har Service = kategoriya (yig'iladigan),
                har Method = card:
    Header:  [stream-badge] Service/Method     (badge: unary|server|client|bidi)
    Body:    Input  message  (Example ⇄ Schema toggle, {{var}} qo'llab-quvvatlash)
             Output message  (Example ⇄ Schema)
             [Copy as grpcurl] [Execute]   // Execute server orqali proxy qilinadi
    Pastda:  gRPC Messages bo'limi (REST Models kabi, $ref havolalar)

Tab holati localStorage'da ("specter.activeTab").
gRPC yo'q bo'lsa (grpc.json bo'sh) -> gRPC tab ko'rsatilmaydi yoki "no protos" hint.
```

grpcurl buyrug'i:
```
grpcurl -plaintext \
  -d '{{interpolated input JSON}}' \
  {{host}}:{{port}} {package}.{Service}/{Method}
```
`host`/`port` env o'zgaruvchilaridan (`{{grpcHost}}`, default `localhost:50051`).

## Bosqichlar

1. `emicklei/proto` qo'shish + `internal/proto` parser (message/enum/service -> GrpcDoc) + test (testdata `.proto`).
2. `specter.Generate`/`Handler`ga `/grpc.json` + `GrpcDoc` (faqat ishlatilgan message).
3. UI tab-view: REST | gRPC almashish; gRPC service = kategoriya, method = card, schema toggle.
4. **Copy as grpcurl** + env `grpcHost`.
5. gRPC Messages bo'limi + `$ref` havolalar.
6. (keyingi) grpc-web orqali haqiqiy Execute — alohida bosqich, ehtimoliy proxy.

## Misol / testdata

`examples/shop/proto/shop.proto`:
```
syntax = "proto3";
package shop.v1;

message User { int32 id = 1; string name = 2; string email = 3; repeated string roles = 4; }
message GetUserRequest { int32 id = 1; }
message ListUsersRequest { string q = 1; int32 limit = 2; }
message ListUsersResponse { repeated User users = 1; int32 total = 2; }

service UserService {
  rpc GetUser(GetUserRequest) returns (User);
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
  rpc StreamUsers(ListUsersRequest) returns (stream User);   // server-streaming
}
```
Kutiladi: gRPC tab -> "UserService" kategoriyasi, 3 method (2 unary + 1 server-stream),
Messages: User, GetUserRequest, ListUsersRequest, ListUsersResponse.

## Cheklovlar (ataylab)

- Import'lar orasidagi type-resolve boshda oddiy (bir paket / nom bo'yicha); to'liq
  FileDescriptor resolve keyin.
- Execute BAJARILDI (`internal/grpcx/invoke.go`), lekin brauzerdan emas: konsol
  Specter serveriga so'rov yuboradi, server `grpcurl` kutubxonasi bilan chaqiradi
  va javobni qaytaradi. Brauzerdan to'g'ridan-to'g'ri gRPC hamon mumkin emas.
  Hozirgi cheklovlari: unary va server-streaming ishlaydi (tekshirilgan);
  client-stream/bidi konsoldan chaqirilmaydi, chunki UI bitta xabar yuboradi.
  Faqat plaintext (TLS yo'q), 15s timeout.
- oneof/Any/well-known types soddalashtirilgan.
```
