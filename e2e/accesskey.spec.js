// Verifies the AccessKey gate in a real browser. The Go tests cover the
// handler's decisions; what they cannot show is whether the console still
// works once gated — the page fetches openapi.json and friends on its own, and
// those requests have to carry the key without the page knowing about it.
const { chromium } = require('playwright');
const { Checks } = require('./lib');

module.exports = async function run(base, key) {
  const c = new Checks('access key');
  const browser = await chromium.launch();

  c.section('[1] Without a key');
  {
    const page = await browser.newPage();
    await page.goto(base, { waitUntil: 'networkidle' });
    const html = await page.content();
    c.check('console is not served', !html.includes('id="exportBtn"'));
    c.check('no endpoints leak', !html.includes('class="op'), html.slice(0, 120));
    await page.close();
  }

  c.section('[2] With the key in the URL');
  const ctx = await browser.newContext();
  {
    const page = await ctx.newPage();
    const jsErrors = [];
    const failed = [];
    page.on('pageerror', e => jsErrors.push(String(e)));
    page.on('response', r => {
      if (r.url().includes('/docs/') && r.status() >= 400) {
        failed.push(`${r.status()} ${r.url()}`);
      }
    });

    await page.goto(`${base}?key=${encodeURIComponent(key)}`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(1000);

    c.check('console is served', (await page.content()).includes('id="exportBtn"'));
    c.check('spec was fetched', (await page.textContent('#title')) !== 'API');
    c.check('endpoints rendered', (await page.locator('.op').count()) > 10);
    // The page's own fetches are relative and carry no key; the cookie is what
    // makes them work, so a 401/404 here means the gate broke the console.
    c.check('no failed requests', failed.length === 0, failed.join(', '));
    c.check('no JS errors', jsErrors.length === 0, jsErrors.join('; '));
    await page.close();
  }

  c.section('[3] Cookie keeps the session');
  {
    const page = await ctx.newPage();
    await page.goto(base, { waitUntil: 'networkidle' });
    await page.waitForTimeout(800);
    c.check('reload without the key still works',
      (await page.textContent('#title')) !== 'API');
    await page.close();
  }

  c.section('[4] A wrong key is refused');
  {
    const page = await (await browser.newContext()).newPage();
    await page.goto(`${base}?key=definitely-wrong`, { waitUntil: 'networkidle' });
    c.check('console is not served', !(await page.content()).includes('id="exportBtn"'));
    await page.close();
  }

  await browser.close();
  return c.summary();
};
