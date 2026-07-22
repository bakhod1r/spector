// Drives the console's Realtime tab against real servers: a real SSE stream,
// a real WebSocket, and a real MQTT broker (mosquitto). The MQTT path is the
// point of this script — the client codec is hand-written, and mosquitto is
// an independent implementation, so a shared misunderstanding cannot pass.
const { chromium } = require('playwright');
const { Checks } = require('./lib');

const paneOf = (page, tag) => page.locator(`#cat-${tag}`);

async function logText(pane) {
  return (await pane.locator('.log').textContent()) || '';
}

// waitFor polls the pane log until the pattern shows up or time runs out.
async function waitForLog(pane, re, ms = 12000) {
  const deadline = Date.now() + ms;
  while (Date.now() < deadline) {
    if (re.test(await logText(pane))) return true;
    await new Promise(r => setTimeout(r, 200));
  }
  return false;
}

async function isLive(pane) {
  return (await pane.locator('.dot.live').count()) > 0;
}

// Drives the Realtime tab against real servers. The MQTT path is the point:
// the client codec is hand-written, and mosquitto is an independent
// implementation, so a shared misunderstanding cannot pass.
module.exports = async function run(BASE, MQTT_WS) {
  const c = new Checks('realtime');
  const check = (n, ok, d) => c.check(n, ok, d);
  {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  const jsErrors = [];
  page.on('pageerror', e => jsErrors.push(String(e)));

  await page.goto(BASE + '#/realtime', { waitUntil: 'networkidle' });
  await page.waitForTimeout(800);

  // ---- WebSocket ----
  c.section('[1] WebSocket');
  {
    const pane = paneOf(page, 'WebSocket');
    await pane.getByRole('button', { name: 'Connect', exact: true }).click();
    check('connected', await waitForLog(pane, /open|connected/i), (await logText(pane)).slice(0, 150));
    check('dot is live', await isLive(pane));
    check('received a message', await waitForLog(pane, /"seq"\s*:\s*1/),
          (await logText(pane)).slice(0, 250));
    check('received several', await waitForLog(pane, /"seq"\s*:\s*3/),
          (await logText(pane)).slice(0, 300));

    await pane.getByRole('button', { name: 'Disconnect', exact: true }).click();
    await page.waitForTimeout(500);
    check('disconnect clears live state', !(await isLive(pane)));
  }

  // ---- SSE ----
  c.section('[2] SSE');
  {
    const pane = paneOf(page, 'SSE');
    // EventSource delivers named events only to matching listeners, so the
    // pane asks which names to subscribe to. Our endpoint emits "tick".
    await pane.locator('input.mono').nth(1).fill('tick');
    await pane.getByRole('button', { name: 'Connect', exact: true }).click();
    check('connected', await waitForLog(pane, /open|connected/i), (await logText(pane)).slice(0, 150));
    check('received tick 1', await waitForLog(pane, /"seq"\s*:\s*1/),
          (await logText(pane)).slice(0, 250));
    check('received tick 3', await waitForLog(pane, /"seq"\s*:\s*3/),
          (await logText(pane)).slice(0, 300));
    // Named events only reach matching listeners, which the pane handles.
    check('named event labelled', /\[tick\]/.test(await logText(pane)),
          (await logText(pane)).slice(0, 200));
    check('stream stays open (no reconnect storm)',
          !/retrying/.test(await logText(pane)), (await logText(pane)).slice(0, 200));

    await pane.getByRole('button', { name: 'Disconnect', exact: true }).click();
    await page.waitForTimeout(500);
    check('disconnected', !(await isLive(pane)));
  }

  // ---- MQTT ----
  c.section('[3] MQTT (hand-written codec vs mosquitto)');
  {
    const pane = paneOf(page, 'MQTT');
    await pane.locator('input.mono').first().fill(MQTT_WS);

    await pane.getByRole('button', { name: 'Connect', exact: true }).click();
    // CONNECT -> CONNACK is the codec's first real exchange with the broker.
    check('CONNACK accepted', await waitForLog(pane, /connack|connected|open/i),
          (await logText(pane)).slice(0, 250));
    check('dot is live', await isLive(pane), (await logText(pane)).slice(0, 250));

    // Subscribe, then publish to the same topic and expect it back.
    const topicInputs = pane.locator('input.mono');
    const n = await topicInputs.count();
    console.log(`       (${n} inputs in the MQTT pane)`);

    check('SUBSCRIBE acknowledged', await waitForLog(pane, /subscribed/i),
          (await logText(pane)).slice(0, 300));

    // Publishing goes through the pane's payload box + Send button. The
    // subscription is "#", so whatever we publish must come back from the
    // broker — that round-trip is what proves encode and decode.
    const marker = 'specter-probe-' + Date.now();
    await pane.locator('textarea').first().fill(marker);
    await pane.getByRole('button', { name: 'Send', exact: true }).click();

    check('PUBLISH sent', await waitForLog(pane, new RegExp('→.*' + marker)),
          (await logText(pane)).slice(0, 400));
    check('PUBLISH echoed back by broker',
          await waitForLog(pane, new RegExp('←.*' + marker)),
          (await logText(pane)).slice(0, 500));

    // A payload longer than 127 bytes crosses the remaining-length varint
    // boundary, which is where a hand-written codec usually breaks.
    const big = 'x'.repeat(300);
    await pane.locator('textarea').first().fill(big);
    await pane.getByRole('button', { name: 'Send', exact: true }).click();
    check('multi-byte remaining-length round-trips',
          await waitForLog(pane, new RegExp('←.*' + 'x'.repeat(60))),
          (await logText(pane)).slice(-300));

    console.log('\n--- MQTT log ---');
    console.log((await logText(pane)).slice(0, 800));
    console.log('----------------');

    await pane.getByRole('button', { name: 'Disconnect', exact: true }).click();
    await page.waitForTimeout(500);
    check('disconnected', !(await isLive(pane)));
  }

  console.log('\n[JS errors]', jsErrors.length ? jsErrors : 'none');
  check('no uncaught JS errors', jsErrors.length === 0, jsErrors.join('; '));

  await browser.close();
  }
  return c.summary();
};
