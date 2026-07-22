// Drives the dependency chips against examples/deps, whose handlers really do
// reach a database, a cache, an HTTP service and a queue.
//
// The risk this feature carries is not that it renders wrong — it is that it
// renders confidently wrong. So the checks below are as much about what must
// NOT appear (a handler that reaches nothing showing chips; a guess presented
// as a fact) as about what must.
const { chromium } = require('playwright');
const { Checks } = require('./lib');

module.exports = async function run(BASE) {
  const c = new Checks('calls');
  const check = (n, ok, d) => c.check(n, ok, d);

  const browser = await chromium.launch();
  const page = await browser.newPage();
  const jsErrors = [];
  page.on('pageerror', e => jsErrors.push(String(e)));
  await page.goto(BASE, { waitUntil: 'networkidle' });

  // ---- 1. The spec carries the calls ----
  c.section('[1] Spec');
  const spec = await page.evaluate(() => SPEC);
  const byId = {};
  for (const methods of Object.values(spec.paths)) {
    for (const op of Object.values(methods)) byId[op.operationId] = op;
  }
  const calls = id => byId[id]?.['x-specter-calls'] || [];
  const kinds = id => calls(id).map(x => x.kind).sort();

  check('checkout reaches all four kinds',
        JSON.stringify(kinds('checkout')) === JSON.stringify(['cache', 'db', 'http', 'queue']),
        JSON.stringify(kinds('checkout')));

  // The handler calls ordersFor, which calls queryOrders, which queries. If the
  // walk stopped at the handler body this would be empty.
  check('a dependency two calls down is found',
        kinds('listOrders').includes('db'), JSON.stringify(kinds('listOrders')));

  // Silence has to be reportable. A map that marks everything as reaching
  // something is not a map.
  check('a handler that reaches nothing has no calls',
        calls('health').length === 0, JSON.stringify(calls('health')));

  // ---- 2. Confidence is honest ----
  c.section('[2] Confidence');
  const http = calls('checkout').find(x => x.kind === 'http');
  const db = calls('checkout').find(x => x.kind === 'db');
  check('an imported package is certain', http && http.confidence === 'certain',
        JSON.stringify(http));
  check('a receiver name is only likely', db && db.confidence === 'likely',
        JSON.stringify(db));

  // ---- 3. Rendering ----
  c.section('[3] Chips');
  const anchor = 'op-post--api-checkout-id-';
  await page.goto(`${BASE}#/rest/${anchor}`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(400);

  const view = await page.evaluate(a => {
    const card = document.querySelector(`#${a}`);
    if (!card) return null;
    const chips = [...card.querySelectorAll('.dep')];
    return {
      count: chips.length,
      labels: chips.map(e => e.textContent),
      dashed: chips.filter(e => e.classList.contains('likely')).map(e => e.textContent),
      kindClasses: chips.map(e => [...e.classList].find(k => k.startsWith('k-'))),
      hasNote: !!card.querySelector('.depnote'),
      dots: chips.filter(e => e.querySelector('.dot')).length,
    };
  }, anchor);

  check('card found', !!view, anchor);
  check('one chip per call', view.count === calls('checkout').length,
        `${view.count} vs ${calls('checkout').length}`);
  check('every chip names its target',
        calls('checkout').every(x => view.labels.includes(x.target)),
        JSON.stringify(view.labels));
  check('every chip carries its kind',
        view.kindClasses.every(Boolean), JSON.stringify(view.kindClasses));
  check('every chip has a colour dot', view.dots === view.count);

  // A guess that looks identical to a fact is the failure this design exists to
  // prevent, so the distinction has to survive into the DOM.
  check('guesses are drawn differently', view.dashed.length === 3, JSON.stringify(view.dashed));
  check('the certain one is not dashed', !view.dashed.includes('http.Post'));
  check('the difference is explained', view.hasNote);

  // ---- 4. A handler with no dependencies shows no section ----
  c.section('[4] Silence renders as silence');
  await page.goto(`${BASE}#/rest/op-get--api-health`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(300);
  const healthChips = await page.evaluate(() =>
    document.querySelectorAll('#op-get--api-health .dep').length);
  check('health shows no chips', healthChips === 0, String(healthChips));

  c.section('[5] Console');
  check('no page errors', jsErrors.length === 0, jsErrors.join(' | '));

  await browser.close();
  return c.summary();
};
