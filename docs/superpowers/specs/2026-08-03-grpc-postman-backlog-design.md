# Specter — gRPC + Postman backlog: qolgan cheklovlarni yopish

Sana: 2026-08-03

## Maqsad

`docs/grpc-plan.md` va `docs/postman-plan.md` numbered bosqichlari bitgan.
Qolgani — ataylab keyinga qoldirilgan cheklovlar. Bu spec ularni bitta
backlog sifatida to'playdi, har birini alohida mini-design bilan, implement
tartibida. Har item mustaqil implement/test/commit qilinadi.

Uslub o'zgarmaydi: server minimal, UI bitta self-contained `internal/ui/ui.html`
(tashqi CDN yo'q), parse-metama'lumot falsafasi, mavjud test fayllarga qo'shiladi.

## Implement tartibi

gRPC bloki (G1→G4), keyin Postman bloki (P1→P3). Tartib mezoni: foydalanuvchi
og'rig'i + qiymat/harajat + subsystem to'liqligi birlashtirilgan.

---

## G1 — client-stream / bidi Execute

**Og'riq: yuqori.** Hozir stream method konsoldan to'liq chaqirilmaydi.

Current:
- `internal/grpcx/invoke.go:64` — `grpcurl.InvokeRPC(..., rf.Next)`. `rf.Next`
  allaqachon ko'p xabar oqiydi (backend tayyor).
- `internal/grpcx/invoke.go:51-55` — `RequestParserAndFormatter` `strings.NewReader(req.Data)`dan o'qiydi.
- `internal/ui/ui.html:1543` `invokeGrpc()`, payload `{target, symbol, data}`
  (`:1552`) — UI bitta JSON yuboradi.

O'zgarish:
- Backend: `req.Data` bir nechta JSON obyektni qabul qilsin. grpcurl JSON
  formatida newline/concatenated obyektlarni `rf.Next` orqali ketma-ket
  o'qiydi — `Data` matnida bir nechta obyekt bo'lsa client-stream/bidi ishlaydi.
  `Invoke`da qo'shimcha kod shart emas; faqat UI ko'p obyekt yuborishi kerak.
  Tekshirish: `Data` ichida N obyekt → InvokeRPC N marta `rf.Next` chaqiradi.
- UI: method streaming turini biladi (badge: unary|server|client|bidi).
  client/bidi bo'lsa Input bo'limida "＋ Add message" — bir nechta textarea.
  Execute'da ular JSON obyekt sifatida `\n` bilan join qilinib bitta `data`ga
  yuboriladi.

Test: `internal/grpcx/invoke_test.go` — multi-message `Data` client-streaming
method'ga (mock server) N xabar yetkazishini tekshirish. `internal/grpcx/live_test.go`
allaqachon server-stream tekshiradi; client-stream case qo'shiladi.

YAGNI: interaktiv (bir textarea, real-time push) yo'q — barcha xabar oldindan
kiritiladi, bir Execute'da yuboriladi. Bidi javob tartibi grpcurl qaytargancha.

## G2 — TLS

**Harajat: kichik.** Hozir plaintext-only, 15s hardcoded.

Current:
- `internal/grpcx/invoke.go:33` — `insecure.NewCredentials()` yagona variant.
- `internal/grpcx/invoke.go:19-24` — `Request`da TLS toggle yo'q.
- `internal/grpcx/invoke.go:30` — `15*time.Second` hardcoded.

O'zgarish:
- `Request`ga qo'shish:
  ```go
  TLS        bool   `json:"tls,omitempty"`
  Insecure   bool   `json:"insecure,omitempty"`   // TLS, lekin skip cert verify
  TimeoutSec int    `json:"timeoutSec,omitempty"` // 0 -> default 15
  ```
- `:33` shart: `req.TLS` bo'lsa `credentials.NewTLS(&tls.Config{InsecureSkipVerify: req.Insecure})`,
  aks holda hozirgi `insecure.NewCredentials()`.
- `:30` timeout `req.TimeoutSec` (0 → 15).
- UI: gRPC method card / topbarda TLS toggle + env `grpcTLS`, `grpcInsecure`.
  grpcurl buyrug'i generatsiyasida `-plaintext` faqat TLS o'chiq bo'lsa qo'shiladi;
  `Insecure`da `-insecure`.

Test: `internal/grpcx/invoke_test.go` — `Request{TLS:true}` uchun dial creds
tanlovi (unit, real TLS server shart emas: kichik helper credential turini
qaytaradi va tekshiriladi). TimeoutSec o'tishi.

YAGNI: mutual-TLS (client cert), CA fayl yo'li — boshda yo'q. Faqat server-TLS
+ skip-verify.

## G3 — oneof / Any / well-known types

**Schema sifat.** Hozir soddalashtirilgan.

Current:
- `internal/proto/proto.go:98` `messageToSchema()` — faqat NormalField/MapField.
- `internal/proto/proto.go:101-109` — field iteratsiyasida oneof case yo'q.
- `internal/proto/proto.go:126` `fieldSchema()`, `:134` `scalarSchema()`.
- `internal/proto/scalar_test.go:30` — `google.protobuf.Timestamp` generic
  message ref sifatida.

O'zgarish:
- oneof: emicklei/proto AST'da `*proto.Oneof`. Har variant property qilib
  qo'shiladi (hozirgi soddalashtirilgan yondashuv saqlanadi), plus message
  schema'ga marker `x-oneof: [[fieldA, fieldB], ...]` — UI kelajakda "faqat
  bittasi" ko'rsatishi mumkin. Boshda marker + barcha propertiga optional.
- Well-known (to'liq nom bo'yicha tanish):
  - `google.protobuf.Timestamp`, `.Duration` → `{type:string, format:date-time|duration}`
  - `google.protobuf.Struct`, `.Value` → `{type:object}` / bo'sh schema
  - `google.protobuf.Any` → `{type:object, properties:{"@type":{type:string}}}`
  - `google.protobuf.Empty` → `{type:object}`
  - `.StringValue/.Int32Value/...` wrapperlar → tegishli skalar.
  Tanish nomlar map/switch, aks holda hozirgi message-ref fallback.

Test: `internal/proto/scalar_test.go` — Timestamp→date-time, Any→`@type`
property. `internal/proto/proto_test.go` — oneof li message schema'da barcha
variant property + `x-oneof` marker.

YAGNI: nested Any decode, Struct ichini rekursiv chizish yo'q — sathi schema.

## G4 — FileDescriptor import-resolve

**Eng katta.** Hozir bir paket/nom bo'yicha, import kuzatilmaydi.

Current:
- `internal/proto/proto.go:12` `Scan()` entry.
- `internal/proto/proto.go:25` `proto.NewParser().Parse()` (emicklei).
- `internal/proto/proto.go:32-43` `proto.Walk()` — import traversal yo'q.
- `internal/proto/proto.go:62` `protoFiles()` — dir walk.

O'zgarish:
- Scan barcha `.proto`ni yig'adi (mavjud), lekin `import "x.proto"` statement'lar
  ochilib, tur nomlari to'g'ri file/paket bo'yicha resolve qilinadi. emicklei AST
  `*proto.Import` beradi.
- Cross-file message ref: hozir bir umumiy `Messages` mapga nomga yig'iladi; to'liq
  paket-qualified nom (`pkg.Message`) bilan kalitlash, `$ref` shunga ishoralaydi.
  Bir xil nom har xil paketda to'qnashmasin.
- Import grafi: fayl A B'ni import qilsa, B message'lari A service input/outputidan
  ko'rinadi. Yopiq to'plam collect (REST'dagi kabi) paket-qualified nom ustidan.

Test: `internal/proto/proto_test.go` — testdata'ga import qiluvchi 2-fayl qo'shish
(`common.proto` + uni import qiluvchi `shop.proto`), cross-file ref to'g'ri
ulanishini tekshirish. `internal/grpcx/invoke.go:79` `DescriptorSourceFromProtoFiles`
allaqachon protoc-toolchainsiz import resolve qiladi (grpcurl ichida) — Invoke
tomoni tekshiriladi.

YAGNI: proto2, `option` semantikasi, custom option resolve yo'q. Faqat import +
tur resolve.

---

## P1 — Full JSONPath

**Harajat: kichik-o'rta.** Hozir dotted/index.

Current:
- `internal/ui/ui.html:881` `pick()` — `path.replace(/^\$\.?/,"").split(/[.\[\]]/).filter(Boolean)`,
  obyekt daraxtini yuradi. `$.id`, `$.a.b`, `$[0]`, `$.items[0].name` ishlaydi.
- Chaqiruvchilar: `:891-892` `runTest()` (jsonExists/jsonEquals), `:920` `send()`
  extract-chaining loop.
- Editor: `:1144` `extractEditor()`.

O'zgarish:
- `pick()`ni kichik JSONPath evaluatorga kengaytirish (self-contained, tashqi
  kutubxona yo'q):
  - wildcard `[*]` va `.*` — massiv/obyekt bo'ylab
  - recursive descent `..key`
  - filter `[?(@.key=='val')]` — oddiy tenglik/`==`,`!=`,mavjudlik
  - index/slice `[0]`, `[-1]` saqlanadi
- Ko'p qiymat qaytishi mumkin (wildcard/filter): extract birinchisini oladi,
  test jsonExists "kamida bitta" bo'ladi. API: `pick()` massiv qaytaradi, hozirgi
  chaqiruvchilar `[0]`/length bilan moslashtiriladi.

Test: `internal/ui/ui_test.go` yoki `e2e/console.spec.js` — `$.users[*].name`,
`$..id`, `$.users[?(@.role=='admin')].name` misollari; chaining + assert ishlashi.

YAGNI: to'liq JSONPath grammatikasi (union `[a,b]`, script expr, math) yo'q.
Yuqoridagi 4 kengaytma yetarli.

## P2 — Postman v2.1 import

**Harajat: o'rta.** Hozir faqat o'z format.

Current:
- `internal/ui/ui.html:303` `EXPORT_FORMAT="specter.collection"`.
- `:319` `parseImport()` — format/version validatsiya (`:324-326`).
- `:339` `mergeStore()`, `:361` `exportStore()`, `:371` `importStore()`
  (replace/merge confirm dialog `:378-382`).
- Store: `{format,version,exportedAt,activeEnvId,environments[],collections[],history[]}`.

O'zgarish:
- `importStore()`da format detektsiya: JSON'da `info` + `item` (yoki
  `info.schema` `.../v2.1.0/...`) bo'lsa → Postman mapper, aks holda hozirgi
  `parseImport`.
- Postman v2.1 → Store mapper:
  - `item[]` (folder rekursiv) → `Collection.requests[]` (`SavedRequest`)
  - `request.method` → `method`; `request.url.raw`/`path` → `path`;
    `request.url.query[]` → `queryParams`; `request.header[]` → `headers`
  - `request.body.raw` (mode=raw) → `body`; boshqa mode (formdata/urlencoded)
    boshlang'ich: raw yig'ish yoki bo'sh + warning
  - `request.auth` → `Auth` (bearer/basic/apikey map)
  - `{{var}}` Postman o'zgaruvchilari o'zgarmay saqlanadi (bir xil sintaksis)
  - `event[]` (`prerequest`/`test` script) → P3 hook'iga yoziladi (P3 bo'lsa),
    aks holda vaqtincha `SavedRequest.notes`ga saqlanadi (yo'qotilmasin).
- Import merge/replace hozirgi dialog orqali.

Test: `e2e/console.spec.js` — kichik Postman v2.1 collection JSON import →
kutilgan `SavedRequest`lar (method/url/header/auth). Bad/aralash format
rad etilmasligi (o'z format hamon ishlaydi).

YAGNI: formdata/file body, Postman environment `.postman_environment.json`
alohida import, script konvertatsiya (Postman `pm.*` → bizniki) yo'q. Faqat
collection struktura + raw body + auth.

## P3 — Pre-request script (JS sandbox)

**Xavfsizlik-sezgir. Oxirgi.** Hozir umuman yo'q (mapping tasdiqladi).

Current:
- `internal/ui/ui.html:428-431` — faqat `{{var}}` interpolatsiya. Script/eval/Function yo'q.
- `send()` — so'rov qurish/yuborish (buildRequest → fetch).

O'zgarish:
- `SavedRequest`ga `preRequestScript: string` maydoni. Editor'da yangi tab/maydon.
- `send()`da, `buildRequest`dan **oldin**, script bo'lsa sandbox'da bajariladi:
  ```
  const fn = new Function("pm", scriptBody);
  fn(pmApi(env));   // pmApi: cheklangan sirt
  ```
  `pmApi` faqat quyidagilarni beradi:
  - `pm.environment.get(name)` / `.set(name, val)` → aktiv env.vars
  - `pm.variables.get/set` → env.vars (alias)
  - `pm.request` (read-only snapshot: method/path/headers)
  Global (`window`, `fetch`, `document`, `localStorage`) sirtga chiqmaydi —
  `Function` argumentiga faqat `pm` beriladi; `"use strict"` prepend.

### Xavfsizlik eslatmasi (spec sathida ochiq)

`new Function`/`eval` foydalanuvchi kiritgan kodni ishga tushiradi. Bu **bir
xil origin ichida bajariladi** — `Function` argument-cheklovi global'ni yashiradi,
lekin haqiqiy JS sandbox emas: yaxshi niyatli foydalanuvchini himoya qiladi,
adversarial kodni EMAS (masalan `this`, konstruktor zanjiri orqali global'ga
yetish mumkin). Shuning uchun:
- Script foydalanuvchining O'ZI yozadi/import qiladi — o'z konsolida o'z kodi.
- Import (P2) orqali kelgan begona script AVTOMATIK bajarilmaydi: birinchi
  Execute'da "run imported script?" tasdiq so'raladi (yoki default o'chiq,
  foydalanuvchi yoqadi).
- Spec'da xavf hujjatlashtirilgan; to'liq izolyatsiya (Web Worker + qat'iy
  proxy, yoki iframe sandbox) kelajak ish sifatida belgilanadi.

Test: `e2e/console.spec.js` — script `pm.environment.set('token','x')` → keyingi
so'rovda `{{token}}` interpolatsiyada `x`. Global'ga kirmasligi (masalan
`pm.window` undefined).

YAGNI: post-response script (test script'lar allaqachon assert bilan qoplangan),
`pm.sendRequest`, tashqi kutubxona, npm-uslub require yo'q.

---

## Umumiy cheklovlar

- Har item mustaqil: alohida commit, mavjud test fayllarga qo'shiladi.
- Server sirt minimal o'sadi (faqat `Request` maydonlari G1/G2).
- UI bitta fayl, tashqi asset yo'q — barcha yangi JS ichkarida.
- To'liq izolyatsiya (P3), mutual-TLS (G2), to'liq JSONPath grammatikasi (P1),
  proto2/custom-option (G4) — ataylab tashqarida.
