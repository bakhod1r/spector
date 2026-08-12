// Isolated check for the Run all request/response detail panel, so a
// pre-existing failure in another console section does not mask this one.
const { chromium } = require('playwright');
const { spawn, spawnSync } = require('child_process');
const path = require('path');

const REPO = path.join(__dirname, '..');
const PORT = 8767;
const BASE = `http://127.0.0.1:${PORT}/docs/`;

(async () => {
  const bin = path.join(__dirname, '.shop-server');
  const b = spawnSync('go', ['build', '-o', bin, './examples/shop'], { cwd: REPO, stdio: 'inherit' });
  if (b.status !== 0) process.exit(2);
  const srv = spawn(bin, [], { cwd: path.join(REPO, 'examples', 'shop'), env: { ...process.env, PORT: String(PORT), ADDR: `:${PORT}`, GIN_MODE: 'release' }, stdio: 'ignore' });

  const ok = async () => { try { return (await fetch(BASE + 'openapi.json')).ok; } catch { return false; } };
  for (let i = 0; i < 40 && !(await ok()); i++) await new Promise(r => setTimeout(r, 250));

  let failed = 0;
  const check = (n, cond, d) => { console.log((cond ? 'PASS ' : 'FAIL ') + n + (d ? '  ' + d : '')); if (!cond) failed++; };
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    const jsErrors = []; page.on('pageerror', e => jsErrors.push(String(e)));
    await page.goto(BASE, { waitUntil: 'networkidle' });
    page.once('dialog', d => d.dismiss());
    await page.click('#runAllBtn');
    await page.waitForSelector('#runBody .run-row', { timeout: 10000 });
    const line = await page.$('#runBody .run-row .run-line');
    check('a run row rendered', !!line);
    await line.click();
    const detail = await page.waitForSelector('#runBody .run-detail.on', { timeout: 5000 });
    const txt = await detail.innerText();
    check('Request block', /Request/.test(txt), JSON.stringify(txt.slice(0, 60)));
    check('request line GET', /GET\s/.test(txt), JSON.stringify(txt.slice(0, 80)));
    check('Response block', /Response/.test(txt));
    check('status shown', /status:\s*\d/.test(txt));
    check('no JS errors', jsErrors.length === 0, jsErrors.join('; '));
  } catch (e) {
    console.log('FAIL harness  ' + e); failed++;
  } finally {
    await browser.close();
    srv.kill();
  }
  process.exit(failed ? 1 : 0);
})();
