# Specter — Postman-uslubidagi aqlli UI rejasi

## Maqsad

Hozirgi UI: statik OpenAPI ko'rinishi + Execute. Postman darajasiga olib chiqish:
muhitlar (environments) va o'zgaruvchilar, saqlanadigan so'rovlar (collections),
so'rovlar tarixi, javobdan o'zgaruvchi ajratib keyingi so'rovda ishlatish (chaining),
auth turlari, va oddiy testlar/assertlar. Server tegmaydi — hamma narsa
`internal/ui/ui.html` ichida, holat `localStorage`da (self-contained, tashqi CDN yo'q).

## Arxitektura

- Backend o'zgarmaydi: `specter.Handler` `/openapi.json` va UI beradi.
- Yangi ish faqat frontendda: bitta `ui.html` ichida holat boshqaruvi + `{{var}}`
  interpolatsiya + `localStorage` persistligi.
- OpenAPI spec = so'rovlar shabloni manbasi; foydalanuvchi ustidan variantlar saqlaydi.

## Data schema (pseudocode)

```
// localStorage kaliti: "specter.state" -> JSON(Store)

Store {
  activeEnvId: string
  environments: Environment[]
  collections: Collection[]
  history: HistoryEntry[]   // oxirgi N ta, aylanma
}

Environment {
  id: string
  name: string                 // "local", "staging", "prod"
  vars: Map<string, string>    // {"baseUrl": "http://localhost:8080",
                               //  "token": "abc", "userId": "1"}
}

Collection {
  id: string
  name: string
  requests: SavedRequest[]
}

SavedRequest {
  id: string
  name: string                 // "Create user"
  method: string               // "post"
  path: string                 // "/api/v1/users"  (OpenAPI'dagi shablon)
  pathParams: Map<string,string>   // {"id": "{{userId}}"}
  queryParams: Map<string,string>  // {"q": "ada", "limit": "10"}
  headers: Map<string,string>      // {"X-Trace": "{{traceId}}"}
  body: string                     // JSON matn, {{var}} bo'lishi mumkin
  auth: Auth
  extract: ExtractRule[]           // javobdan o'zgaruvchi olish (chaining)
  tests: Test[]                    // oddiy assertlar
}

Auth {
  type: "none" | "bearer" | "basic" | "apiKey"
  // bearer:  token
  // basic:   username, password
  // apiKey:  name (header nomi), value, in ("header" | "query")
  fields: Map<string,string>       // qiymatlar {{var}} bo'lishi mumkin
}

ExtractRule {
  from: "body" | "header" | "status"
  jsonPath: string          // "$.id" yoki "$.data[0].token" (oddiy dotted path)
  setVar: string            // natijani shu env-o'zgaruvchiga yozadi -> chaining
}

Test {
  kind: "status" | "jsonEquals" | "jsonExists" | "headerEquals"
  target: string            // status uchun bo'sh; json uchun path; header uchun nom
  expected: string
}

HistoryEntry {
  at: timestamp
  method, url: string
  status: int
  durationMs: int
  requestSnapshot: SavedRequest   // qayta yuborish uchun
  responseBody: string
}
```

## Asosiy algoritmlar (pseudocode)

```
// 1) O'zgaruvchi interpolatsiya: matndagi {{name}} -> aktiv env qiymati
function interpolate(text, env):
    return text.replaceAll(/{{\s*(\w+)\s*}}/, (m, name) ->
        env.vars[name] ?? m)              // topilmasa o'zini qoldiradi

// 2) So'rov qurish (interpolatsiyadan keyin)
function buildRequest(req, env):
    url  = interpolate(env.vars.baseUrl + subst(req.path, req.pathParams), env)
    url += queryString(interpolateMap(req.queryParams, env))
    hdr  = interpolateMap(req.headers, env)
    applyAuth(req.auth, env, hdr, url)     // bearer/basic/apiKey
    body = req.body ? interpolate(req.body, env) : null
    return { method: req.method, url, headers: hdr, body }

// 3) Yuborish + chaining + testlar
function send(req, env):
    built = buildRequest(req, env)
    t0 = now()
    res = fetch(built.url, built)          // bir origin -> proxy shart emas
    entry = record(history, built, res, now()-t0)

    for rule in req.extract:               // javobdan o'zgaruvchi ajratish
        val = pick(res, rule)              // body JSON / header / status
        if val != null: env.vars[rule.setVar] = val   // keyingi so'rovga tayyor
    saveStore()

    results = [ runTest(t, res) for t in req.tests ]   // pass/fail
    return { res, results, entry }

// 4) OpenAPI'dan boshlang'ich SavedRequest yasash
function fromSpec(path, method, op):
    return SavedRequest {
        method, path,
        pathParams:  { p.name: "" for p in op.params where in=="path" },
        queryParams: { p.name: "" for p in op.params where in=="query" },
        headers:     { p.name: "" for p in op.params where in=="header" },
        body: op.requestBody ? sampleJSON(op.requestBody.schema) : "",
        auth: {type:"none"}, extract: [], tests: []
    }
```

## UI tuzilishi (pseudocode)

```
Layout:
  Topbar: [Environment selector v]  [Manage envs]  [Import/Export JSON]
  Left panel:  Collections tree (folder -> SavedRequest)  +  History tab
  Center:      Request editor  (Params | Headers | Body | Auth | Extract | Tests)
               [Send]  [Save]  [Copy as cURL]
  Bottom:      Response (Status/time/size | Body | Headers | Test results)

Har OpenAPI endpoint yonida "＋ Save as request" -> fromSpec() -> collectionga qo'shadi.
```

## Bosqichlar

1. **Environments + `{{var}}` interpolatsiya** — topbar selektor, baseUrl/token o'zgaruvchilari, so'rov/cURL'da almashtirish. (asosiy qiymat)
2. **Saqlanadigan so'rovlar (collections) + localStorage** — endpointdan Save, tahrirlash, qayta yuborish.
3. **History** — har Send yozib boradi, bir bosishda qayta yuborish.
4. **Auth turlari** — bearer/basic/apiKey, env'dan {{token}}.
5. **Chaining (ExtractRule)** — javob JSON'dan `$.id` -> env var -> keyingi so'rov.
6. **Testlar (assert)** — status/json/header, pass-fail ko'rsatkichi.
7. **Import/Export** — BAJARILDI. Butun Store JSON sifatida
   (`format: "specter.collection"`, `version: 1`). Import merge yoki replace
   tanlovini so'raydi; merge'da id to'qnashuvlari yangi id oladi. Postman v2.1
   formatini o'qish hali yo'q — faqat o'z formatimiz.

## Cheklovlar (ataylab)

- jsonPath faqat oddiy dotted/indeks (`$.a.b[0]`) — to'liq JSONPath emas.
- pre-request script (ixtiyoriy JS) — xavfsizlik uchun boshda yo'q; keyin `Function` sandbox bilan qo'shilishi mumkin.
- Hammasi bitta faylda, tashqi kutubxonasiz.
```

## Realtime (rejadan tashqari qo'shilgan)

Konsolda Realtime tab bor: WebSocket, SSE, MQTT. Bu reja yozilganda
ko'zda tutilmagan edi, kod keyinroq qo'shilgan.

- WebSocket — native `WebSocket`.
- SSE — native `EventSource`. GET-only, custom header yo'q; nomlangan
  hodisalar uchun panelda "Named events" maydoni bor.
- MQTT — `ws://` ustidan, qo'lda yozilgan MQTT 3.1.1 codec bilan (CDN
  client ishlatib bo'lmaydi, chunki Specter tashqi assetsiz bitta fayl).

Tekshirilgan: `examples/shop` ning `/events` va `/ws` endpointlariga hamda
mosquitto brokeriga qarshi, brauzerda haqiqiy kliklar bilan. MQTT uchun
CONNECT/CONNACK, SUBSCRIBE/SUBACK va PUBLISH round-trip ishlaydi, shu
jumladan 300 baytli payload (remaining-length varint chegarasidan o'tadi).
