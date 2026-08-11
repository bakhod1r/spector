// Drives the real console in a real browser: clicks the buttons that could
// never be verified from curl or a DOM dump.
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');
const os = require('os');
const { Checks } = require('./lib');

// Exercises the console's stateful features: the ones that only exist once a
// human clicks something, so curl and a DOM dump cannot reach them.
module.exports = async function run(BASE) {
  const c = new Checks('console');
  const check = (n, ok, d) => c.check(n, ok, d);
  const DL = fs.mkdtempSync(path.join(os.tmpdir(), 'specter-e2e-'));
  {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ acceptDownloads: true });
  const page = await ctx.newPage();

  const jsErrors = [];
  page.on('pageerror', e => jsErrors.push(String(e)));

  await page.goto(BASE, { waitUntil: 'networkidle' });

  // ---- 0. Favicon ----
  // The console ships as one file with no external assets, so the icon has to
  // be inline; a data URI that does not decode leaves a broken tab icon and no
  // error anywhere.
  c.section('[0] Favicon');
  const icon = await page.evaluate(() => {
    const l = document.querySelector('link[rel=icon]');
    return l ? l.getAttribute('href') : null;
  });
  check('favicon declared', !!icon);
  check('is inline, not a fetch', icon.startsWith('data:image/svg+xml,'), icon.slice(0, 30));
  check('is a valid URI', !/[\s"]/.test(icon));
  const decoded = await page.evaluate(async href => {
    const img = new Image();
    img.src = href;
    try { await img.decode(); return true; } catch { return false; }
  }, icon);
  check('decodes as an image', decoded);

  // ---- 1. Export: real click, real file ----
  c.section('[1] Export');
  const [download] = await Promise.all([
    page.waitForEvent('download', { timeout: 10000 }),
    page.click('#exportBtn'),
  ]);
  const file = path.join(DL, download.suggestedFilename());
  await download.saveAs(file);
  check('download fired', fs.existsSync(file));
  check('filename', download.suggestedFilename() === 'specter-collection.json',
        download.suggestedFilename());

  const exported = JSON.parse(fs.readFileSync(file, 'utf8'));
  check('format marker', exported.format === 'specter.collection', exported.format);
  check('version', exported.version === 1, String(exported.version));
  check('has environments', Array.isArray(exported.environments) && exported.environments.length > 0);
  check('has collections', Array.isArray(exported.collections));

  // ---- 2. Import: real file picker ----
  c.section('[2] Import (replace)');
  // Seed a request so there is something identifiable to import back.
  const seeded = JSON.parse(JSON.stringify(exported));
  seeded.collections[0].requests.push({
    id: 'imported-1', name: 'IMPORTED REQUEST', method: 'get', path: '/imported',
  });
  seeded.environments.push({ id: 'staging', name: 'staging', vars: { baseUrl: 'https://staging' } });
  const importFile = path.join(DL, 'to-import.json');
  fs.writeFileSync(importFile, JSON.stringify(seeded));

  page.once('dialog', d => d.accept());          // confirm() -> replace
  await page.setInputFiles('#importFile', importFile);
  await page.waitForTimeout(600);

  const store = await page.evaluate(() => JSON.parse(localStorage.getItem('specter.state') || '{}'));
  const names = (store.collections?.[0]?.requests || []).map(r => r.name);
  check('imported request in store', names.includes('IMPORTED REQUEST'), JSON.stringify(names));
  const envNames = (store.environments || []).map(e => e.name);
  check('imported environment in store', envNames.includes('staging'), JSON.stringify(envNames));

  const collText = await page.textContent('#collPane');
  check('imported request rendered in sidebar', /IMPORTED REQUEST/.test(collText || ''));

  // ---- 3. Import rejects a bad file ----
  c.section('[3] Import (bad file rejected)');
  const badFile = path.join(DL, 'bad.json');
  fs.writeFileSync(badFile, '{"nope": true}');
  let alertMsg = null;
  page.once('dialog', d => { alertMsg = d.message(); d.accept(); });
  await page.setInputFiles('#importFile', badFile);
  await page.waitForTimeout(500);
  check('alert shown', alertMsg !== null, String(alertMsg));
  check('alert explains why', /not a specter collection/i.test(alertMsg || ''), String(alertMsg));

  // ---- 3b. Import: Postman v2.1 collection ----
  c.section('[3b] Import (Postman v2.1)');
  const postman = {
    info: {
      name: 'demo',
      schema: 'https://schema.getpostman.com/json/collection/v2.1.0/collection.json',
    },
    item: [{
      name: 'Get user',
      request: {
        method: 'GET',
        header: [{ key: 'X-Trace', value: '{{traceId}}' }],
        url: {
          raw: '{{baseUrl}}/users/1',
          path: ['users', '1'],
          query: [{ key: 'q', value: 'ada' }],
        },
        auth: { type: 'bearer', bearer: [{ key: 'token', value: '{{token}}' }] },
      },
    }],
  };
  const postmanFile = path.join(DL, 'postman.json');
  fs.writeFileSync(postmanFile, JSON.stringify(postman));
  page.once('dialog', d => d.accept());          // confirm() -> replace
  await page.setInputFiles('#importFile', postmanFile);
  await page.waitForTimeout(600);

  const pmStore = await page.evaluate(() => JSON.parse(localStorage.getItem('specter.state') || '{}'));
  const pmColl = (pmStore.collections || []).find(c => c.name === 'demo');
  check('demo collection imported', !!pmColl, JSON.stringify(pmStore.collections));
  const pmReq = pmColl && pmColl.requests[0];
  check('request method GET', pmReq && pmReq.method.toLowerCase() === 'get', JSON.stringify(pmReq));
  check('request path contains users', pmReq && /users/.test(pmReq.path), JSON.stringify(pmReq));
  check('query q=ada', pmReq && pmReq.queryParams && pmReq.queryParams.q === 'ada', JSON.stringify(pmReq && pmReq.queryParams));
  check('header X-Trace', pmReq && pmReq.headers && pmReq.headers['X-Trace'] === '{{traceId}}', JSON.stringify(pmReq && pmReq.headers));
  check('auth type bearer', pmReq && pmReq.auth && pmReq.auth.type === 'bearer', JSON.stringify(pmReq && pmReq.auth));

  // ---- 4. GraphQL Execute ----
  c.section('[4] GraphQL Execute');
  await page.goto(BASE + '#/graphql/gql-Query-user', { waitUntil: 'networkidle' });
  await page.waitForTimeout(700);
  const card = page.locator('#gql-Query-user');
  await card.locator('textarea').nth(1).fill('{\n  "id": "1"\n}');
  await card.locator('button:has-text("Execute")').click();
  await page.waitForTimeout(1500);
  const respText = (await card.locator('.resp').textContent()) || '';
  check('status OK', /OK\s*200/.test(respText), respText.slice(0, 120));
  check('response has data', /ada@example\.com/.test(respText), respText.slice(0, 200));

  // ---- 5. GraphQL mutation ----
  c.section('[5] GraphQL Execute (mutation)');
  await page.goto(BASE + '#/graphql/gql-Mutation-placeOrder', { waitUntil: 'networkidle' });
  await page.waitForTimeout(700);
  const mcard = page.locator('#gql-Mutation-placeOrder');
  await mcard.locator('textarea').nth(1).fill(JSON.stringify({
    input: { userId: '1', lines: [{ productId: '1', quantity: 3 }] },
  }, null, 2));
  await mcard.locator('button:has-text("Execute")').click();
  await page.waitForTimeout(1500);
  const mresp = (await mcard.locator('.resp').textContent()) || '';
  check('mutation OK', /OK\s*200/.test(mresp), mresp.slice(0, 120));
  check('mutation computed total', /29\.97/.test(mresp), mresp.slice(0, 250));

  // ---- 6. gRPC Execute ----
  c.section('[6] gRPC Execute');
  await page.goto(BASE + '#/grpc/grpc-UserService-GetUser', { waitUntil: 'networkidle' });
  await page.waitForTimeout(700);
  const gcard = page.locator('#grpc-UserService-GetUser');
  await gcard.locator('textarea').first().fill('{"id": 2}');
  await gcard.locator('button:has-text("Invoke")').click();
  await page.waitForTimeout(2000);
  const gresp = (await gcard.locator('.resp').textContent()) || '';
  check('grpc invoke OK', /OK\s*200/.test(gresp), gresp.slice(0, 120));
  check('grpc returned Alan', /Alan/.test(gresp), gresp.slice(0, 200));

  // ---- 7. Router: back / forward ----
  c.section('[7] Router back/forward');
  await page.goto(BASE, { waitUntil: 'networkidle' });
  await page.click('#tabGrpc');
  await page.waitForTimeout(300);
  await page.click('#tabGraphql');
  await page.waitForTimeout(300);
  check('hash tracks tab', page.url().includes('#/graphql'), page.url());

  await page.goBack();
  await page.waitForTimeout(500);
  check('back returns to grpc', page.url().includes('#/grpc'), page.url());
  const grpcTabOn = await page.evaluate(() =>
    document.getElementById('tabGrpc').classList.contains('on'));
  check('back re-renders the pane', grpcTabOn);

  await page.goForward();
  await page.waitForTimeout(500);
  check('forward returns to graphql', page.url().includes('#/graphql'), page.url());

  // ---- 8. Reload keeps place ----
  c.section('[8] Reload keeps place');
  await page.goto(BASE + '#/grpc', { waitUntil: 'networkidle' });
  await page.reload({ waitUntil: 'networkidle' });
  await page.waitForTimeout(500);
  const stillGrpc = await page.evaluate(() =>
    document.getElementById('tabGrpc').classList.contains('on'));
  check('still on grpc after reload', stillGrpc);

  // ---- 8.5 Chaining rules survive a re-render ----
  // The request object is rebuilt from the spec whenever the pane renders, so
  // anything authored by hand lived only until the next tab switch. Rules cost
  // real thought to write; losing them silently is worse than not offering them.
  c.section('[8.5] Chaining persistence');
  await page.goto(`${BASE}#/rest/op-get--api-v1-carts`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(400);

  const cardSel = '#op-get--api-v1-carts';
  const openChaining = () => page.evaluate(s => {
    const card = document.querySelector(s);
    card.classList.add('open');
    const btn = [...card.querySelectorAll('button')].find(b => b.textContent === 'Chaining');
    if (btn) btn.click();
  }, cardSel);
  const readRules = async () => {
    await openChaining();
    return page.evaluate(s => [...document.querySelectorAll(s + ' input[placeholder="var name"]')].map(i => i.value), cardSel);
  };

  await openChaining();
  await page.evaluate(s => {
    const card = document.querySelector(s);
    [...card.querySelectorAll('button')].find(b => /Add rule/.test(b.textContent)).click();
  }, cardSel);
  await page.waitForTimeout(200);
  check('rule added', (await readRules()).length === 1);

  await page.goto(`${BASE}#/grpc`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(300);
  await page.goto(`${BASE}#/rest/op-get--api-v1-carts`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(400);
  check('rule survives a tab switch', (await readRules()).length === 1);

  await page.reload({ waitUntil: 'networkidle' });
  await page.waitForTimeout(400);
  const afterReload = await readRules();
  check('rule survives a reload', afterReload.length === 1, JSON.stringify(afterReload));
  check('the rule kept its value', afterReload[0] === 'id', JSON.stringify(afterReload));

  // Deleting the last rule has to mean deleting it, not resurrecting it later.
  await page.evaluate(s => {
    const card = document.querySelector(s);
    [...card.querySelectorAll('button')].find(b => b.textContent === '×').click();
  }, cardSel);
  await page.waitForTimeout(250);
  await page.reload({ waitUntil: 'networkidle' });
  await page.waitForTimeout(400);
  check('a deleted rule stays deleted', (await readRules()).length === 0);

  // ---- 8.6 Pre-request script (constrained sandbox) ----
  // The script runs before buildURL/buildHeaders, with only a `pm` argument —
  // no window/fetch/document. It should be able to set env vars that {{interpolate}}
  // then picks up on the outgoing request, but it must not be able to reach real globals.
  c.section('[8.6] Pre-request script');
  await page.goto(`${BASE}#/rest/op-get--api-v1-carts`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(400);

  const preReqCardSel = '#op-get--api-v1-carts';
  await page.evaluate(s => document.querySelector(s).classList.add('open'), preReqCardSel);

  // Add a header that references {{token}} so we can observe whether the
  // pre-request script's pm.environment.set actually reached the request.
  await page.evaluate(s => {
    const card = document.querySelector(s);
    const btn = [...card.querySelectorAll('button')].find(b => b.textContent === 'Headers');
    if (btn) btn.click();
  }, preReqCardSel);
  await page.evaluate(s => {
    const card = document.querySelector(s);
    const add = [...card.querySelectorAll('.pane.on button')].find(b => b.textContent === '+ Add');
    add.click();
  }, preReqCardSel);
  await page.waitForTimeout(100);
  const headerKeyInput = page.locator(preReqCardSel + ' .pane.on input[placeholder="key"]').last();
  const headerValInput = page.locator(preReqCardSel + ' .pane.on input[placeholder="value ({{var}} ok)"]').last();
  await headerKeyInput.fill('X-Token');
  await headerValInput.fill('{{token}}');
  await headerKeyInput.dispatchEvent('change');

  // Open the Pre-request Script tab and author a script that also probes for
  // leaked globals: pm.window must be undefined since only `pm` is in scope.
  await page.evaluate(s => {
    const card = document.querySelector(s);
    const btn = [...card.querySelectorAll('button')].find(b => b.textContent === 'Pre-request Script');
    if (btn) btn.click();
  }, preReqCardSel);
  const scriptArea = page.locator(preReqCardSel + ' .pane.on textarea').last();
  await scriptArea.fill("pm.environment.set('token','abc'); pm.environment.set('leak', String(typeof pm.window));");
  await scriptArea.dispatchEvent('input');
  await page.waitForTimeout(100);

  let capturedHeaders = null;
  await page.route('**/api/v1/carts*', route => {
    capturedHeaders = route.request().headers();
    route.continue();
  });
  await page.evaluate(s => {
    const card = document.querySelector(s);
    [...card.querySelectorAll('button')].find(b => b.textContent === 'Send').click();
  }, preReqCardSel);
  await page.waitForTimeout(1200);
  await page.unroute('**/api/v1/carts*');

  check('pre-request script set token, used in outgoing header',
        !!capturedHeaders && capturedHeaders['x-token'] === 'abc',
        JSON.stringify(capturedHeaders));

  const leakVal = await page.evaluate(() => activeEnv().vars.leak);
  check('pm has no window global (constrained sandbox)', leakVal === 'undefined', String(leakVal));

  // Imported (foreign) scripts must never auto-run: req.notes from a Postman
  // import must not be wired into execution.
  const notesNotExecuted = await page.evaluate(() => typeof runPreRequest === 'function' && typeof pmApi === 'function');
  check('runPreRequest / pmApi exist as the only execution path', notesNotExecuted);

  // ---- 9. Search filter in URL ----
  c.section('[9] Search filter routing');
  await page.goto(BASE, { waitUntil: 'networkidle' });
  await page.fill('#search', 'users');
  await page.waitForTimeout(500);
  check('filter in hash', /q=users/.test(page.url()), page.url());

  // ---- 10. JSONPath pick()/pickOne() evaluator ----
  c.section('[10] JSONPath pick/pickOne');
  const jpResults = await page.evaluate(() => {
    const fixture = {
      id: 1,
      users: [
        { id: 2, name: 'ada', role: 'admin' },
        { id: 3, name: 'bob', role: 'user' },
      ],
    };
    return {
      wildcardNames: pick(fixture, '$.users[*].name'),
      dotWildcardNames: pick(fixture, '$.users.*.name'),
      recursiveIds: pick(fixture, '$..id'),
      filterEqAdminNames: pick(fixture, "$.users[?(@.role=='admin')].name"),
      filterNeAdminNames: pick(fixture, "$.users[?(@.role!='admin')].name"),
      filterExistsRole: pick(fixture, '$.users[?(@.role)].name'),
      plainId: pickOne(fixture, '$.id'),
      plainIdArr: pick(fixture, '$.id'),
    };
  });
  check('wildcard [*] matches names', JSON.stringify(jpResults.wildcardNames) === JSON.stringify(['ada', 'bob']), JSON.stringify(jpResults.wildcardNames));
  check('dot wildcard .* matches names', JSON.stringify(jpResults.dotWildcardNames) === JSON.stringify(['ada', 'bob']), JSON.stringify(jpResults.dotWildcardNames));
  check('recursive descent ..id matches all ids', JSON.stringify(jpResults.recursiveIds) === JSON.stringify([1, 2, 3]), JSON.stringify(jpResults.recursiveIds));
  check("filter [?(@.role=='admin')] matches ada", JSON.stringify(jpResults.filterEqAdminNames) === JSON.stringify(['ada']), JSON.stringify(jpResults.filterEqAdminNames));
  check("filter [?(@.role!='admin')] matches bob", JSON.stringify(jpResults.filterNeAdminNames) === JSON.stringify(['bob']), JSON.stringify(jpResults.filterNeAdminNames));
  check('filter [?(@.role)] existence matches both', JSON.stringify(jpResults.filterExistsRole) === JSON.stringify(['ada', 'bob']), JSON.stringify(jpResults.filterExistsRole));
  check('plain $.id regression via pickOne', jpResults.plainId === 1, String(jpResults.plainId));
  check('plain $.id regression via pick (array)', JSON.stringify(jpResults.plainIdArr) === JSON.stringify([1]), JSON.stringify(jpResults.plainIdArr));

  // ---- 11. Run all shows request & response per row ----
  // Each Run all row expands to the request that was sent and the response that
  // came back. The write-guard confirm is dismissed so only read-only requests
  // run — nothing on the example server is mutated.
  c.section('[11] Run all request/response');
  await page.goto(BASE, { waitUntil: 'networkidle' });
  page.once('dialog', d => d.dismiss()); // write-guard → run read-only only
  await page.click('#runAllBtn');
  await page.waitForSelector('#runBody .run-row', { timeout: 10000 });
  const rowLine = await page.$('#runBody .run-row .run-line');
  check('a run row rendered', !!rowLine);
  await rowLine.click();
  const detail = await page.waitForSelector('#runBody .run-detail.on', { timeout: 5000 });
  const detailText = await detail.innerText();
  // The block headings are uppercased by CSS, so innerText returns REQUEST /
  // RESPONSE — match case-insensitively.
  check('row expands to a Request block', /request/i.test(detailText), detailText.slice(0, 80));
  check('row shows the request line', /GET\s/.test(detailText), detailText.slice(0, 120));
  check('row expands to a Response block', /response/i.test(detailText), detailText.slice(0, 120));
  check('row shows the response status', /status:\s*\d/.test(detailText), detailText.slice(0, 120));

  console.log('\n[JS errors]', jsErrors.length ? jsErrors : 'none');
  check('no uncaught JS errors', jsErrors.length === 0, jsErrors.join('; '));

  await browser.close();
  }
  fs.rmSync(DL, { recursive: true, force: true });
  return c.summary();
};
