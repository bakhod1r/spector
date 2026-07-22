// Drives the "View source" panel in a real browser.
//
// The claim this feature makes is specific: the code shown is the code that
// serves the endpoint. Checking that a panel appeared would not test that at
// all, so every check here compares what the console rendered against the
// handler name the spec says the operation has.
const { chromium } = require('playwright');
const { Checks } = require('./lib');

module.exports = async function run(BASE) {
  const c = new Checks('source');
  const check = (n, ok, d) => c.check(n, ok, d);

  const browser = await chromium.launch();
  const page = await browser.newPage();
  const jsErrors = [];
  page.on('pageerror', e => jsErrors.push(String(e)));

  await page.goto(BASE, { waitUntil: 'networkidle' });

  // ---- 1. The spec carries positions at all ----
  c.section('[1] Spec');
  const spec = await page.evaluate(() => SPEC);
  const ops = [];
  for (const [p, methods] of Object.entries(spec.paths)) {
    for (const [m, op] of Object.entries(methods)) ops.push({ p, m, op });
  }
  const withSource = ops.filter(o => o.op['x-specter-source']);
  check('every operation has a source', withSource.length === ops.length,
        `${withSource.length}/${ops.length}`);
  check('paths are relative', withSource.every(o => !o.op['x-specter-source'].file.startsWith('/')));
  check('lines are positive', withSource.every(o => o.op['x-specter-source'].line > 0));

  // ---- 2. The button exists and opens a snippet ----
  c.section('[2] View source');
  const target = ops.find(o => o.op.operationId === 'listCarts') || withSource[0];
  const anchor = `op-${target.m}-${target.p.replace(/[^a-zA-Z0-9]+/g, '-')}`;

  // Open the card via the URL router rather than by hunting for it in the DOM.
  await page.goto(`${BASE}#/rest/${anchor}`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(400);

  const card = await page.$(`#${anchor}`);
  check('card found', !!card, anchor);

  const btn = await card.$('button:has-text("View source")');
  check('button present', !!btn);

  await btn.click();
  await page.waitForSelector(`#${anchor} .srcbox`, { timeout: 5000 });

  // ---- 3. The snippet is the right code ----
  c.section('[3] Correctness');
  const shown = await page.evaluate(a => {
    const box = document.querySelector(`#${a} .srcbox`);
    const hit = box.querySelector('.srclines div.hit');
    return {
      path: box.querySelector('.srcpath').textContent,
      highlighted: hit ? hit.textContent : null,
      lineCount: box.querySelectorAll('.srclines div').length,
      firstNum: parseInt(box.querySelector('.srcnums div').textContent, 10),
      hitNum: parseInt(box.querySelector('.srcnums div.hit').textContent, 10),
    };
  }, anchor);

  const src = target.op['x-specter-source'];
  check('header names the file and line', shown.path === `${src.file}:${src.line}`, shown.path);
  check('one line is highlighted', shown.highlighted !== null);
  check('highlighted line declares the handler',
        shown.highlighted && shown.highlighted.includes(target.op.operationId),
        `${shown.highlighted} (want ${target.op.operationId})`);
  check('gutter number matches the spec', shown.hitNum === src.line,
        `${shown.hitNum} vs ${src.line}`);
  check('context is shown around it', shown.lineCount > 5, String(shown.lineCount));
  check('numbering starts where the snippet does', shown.firstNum <= src.line);

  // ---- 4. Toggle closes it ----
  c.section('[4] Toggle');
  await btn.click();
  await page.waitForTimeout(200);
  const gone = await page.evaluate(a => !document.querySelector(`#${a} .srcbox`), anchor);
  check('second click hides the panel', gone);

  // ---- 5. Code is inserted as text, never as markup ----
  c.section('[5] Escaping');
  const escaped = await page.evaluate(() => {
    const box = document.createElement('div');
    box.appendChild(sourceView({
      file: 'x.go', start: 1, line: 1,
      lines: ['// <img src=x onerror="window.__XSS=1">', 'func f() {}'],
    }));
    document.body.appendChild(box);
    const injected = box.querySelector('img') !== null;
    const text = box.querySelector('.srclines div').textContent;
    box.remove();
    return { injected, text, flag: window.__XSS };
  });
  check('markup in code is not parsed as HTML', escaped.injected === false);
  check('it is shown literally', escaped.text.includes('<img'), escaped.text);
  check('no script ran', escaped.flag === undefined);

  // ---- 6. No JS errors throughout ----
  c.section('[6] Console');
  check('no page errors', jsErrors.length === 0, jsErrors.join(' | '));

  await browser.close();
  return c.summary();
};
