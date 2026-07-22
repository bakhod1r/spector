// Runs the browser suites against a freshly built example server.
//
// The server is started here rather than by the caller because a stale binary
// silently produces passing results for code that is not the code under test.
// Building and starting it in-process makes that impossible.

const { spawn, spawnSync } = require('child_process');
const net = require('net');
const path = require('path');

const REPO = path.resolve(__dirname, '..');
const SHOP = path.join(REPO, 'examples', 'shop');
const MQTT_WS = process.env.MQTT_WS || 'ws://localhost:9001';

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

async function waitForServer(url, timeoutMs = 90000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url);
      if (res.ok) return true;
    } catch {
      // not listening yet
    }
    await new Promise(r => setTimeout(r, 300));
  }
  return false;
}

// mqttReachable reports whether a broker is listening, so its absence is a
// clear skip rather than a confusing wall of failures.
function mqttReachable(wsUrl) {
  const m = /^ws:\/\/([^:/]+):(\d+)/.exec(wsUrl);
  if (!m) return Promise.resolve(false);
  const [, host, port] = m;
  return new Promise(resolve => {
    const sock = net.connect({ host, port: Number(port) });
    const done = ok => { sock.destroy(); resolve(ok); };
    sock.once('connect', () => done(true));
    sock.once('error', () => done(false));
    setTimeout(() => done(false), 2000);
  });
}

(async () => {
  // Build first: `go run` would otherwise interleave compiler output with the
  // server's, and a compile error would look like a startup timeout.
  console.log('building the example server…');
  const bin = path.join(REPO, 'e2e', '.shop-server');
  const build = spawnSync('go', ['build', '-o', bin, './examples/shop'], {
    cwd: REPO, stdio: 'inherit',
  });
  if (build.status !== 0) {
    console.error('build failed');
    process.exit(2);
  }

  const servers = [];
  const shutdown = () => {
    for (const s of servers) {
      if (!s.killed) s.kill('SIGKILL');
    }
  };
  process.on('exit', shutdown);
  process.on('SIGINT', () => { shutdown(); process.exit(130); });

  // start brings up the example on its own port. Each suite that needs a
  // different configuration gets its own instance rather than mutating a
  // shared one.
  const start = async (label, env, readyPath = '/docs/openapi.json', opts = {}) => {
    const port = await freePort();
    console.log(`starting ${label} on :${port}…`);
    const proc = spawn(opts.bin || bin, [], {
      cwd: opts.cwd || SHOP,
      env: { ...process.env, PORT: String(port), ADDR: `:${port}`, GIN_MODE: 'release', ...env },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    servers.push(proc);
    const log = [];
    proc.stdout.on('data', d => log.push(d.toString()));
    proc.stderr.on('data', d => log.push(d.toString()));

    if (!await waitForServer(`http://127.0.0.1:${port}${readyPath}`)) {
      console.error(`${label} never became ready. Output:\n` + log.join(''));
      process.exit(2);
    }
    return { port, base: `http://127.0.0.1:${port}/docs/` };
  };

  let exitCode = 0;
  try {
    const { base } = await start('the example server', {});

    const results = [];
    results.push(await require('./console.spec')(base));
    results.push(await require('./source.spec')(base));

    // examples/shop is deliberately in-memory, so it has no dependencies to
    // draw. examples/deps is the fixture with real ones, and it needs its own
    // instance because Specter scans the directory it is started in.
    const depsBin = path.join(REPO, 'e2e', '.deps-server');
    const depsBuild = spawnSync('go', ['build', '-o', depsBin, './examples/deps'], {
      cwd: REPO, stdio: 'inherit',
    });
    if (depsBuild.status !== 0) { console.error('deps build failed'); process.exit(2); }
    const deps = await start('the deps server', {}, '/docs/openapi.json',
      { bin: depsBin, cwd: path.join(REPO, 'examples', 'deps') });
    results.push(await require('./calls.spec')(deps.base));

    // The gate changes how every request is answered, so it needs an instance
    // of its own.
    const KEY = 'e2e-access-key';
    const gated = await start('the gated server', { SPECTER_KEY: KEY },
      `/docs/openapi.json?key=${KEY}`);
    results.push(await require('./accesskey.spec')(gated.base, KEY));

    if (await mqttReachable(MQTT_WS)) {
      results.push(await require('./realtime.spec')(base, MQTT_WS));
    } else {
      console.log(`\n[realtime] SKIPPED — no MQTT broker at ${MQTT_WS}.`);
      console.log('  Start one with: mosquitto -c e2e/mosquitto.conf');
      // Skipping is not passing; make that visible without failing the run.
      results.push(true);
    }

    exitCode = results.every(Boolean) ? 0 : 1;
  } catch (err) {
    console.error('HARNESS ERROR:', err);
    exitCode = 2;
  } finally {
    shutdown();
  }

  console.log(exitCode === 0 ? '\nALL SUITES PASSED' : '\nSUITES FAILED');
  process.exit(exitCode);
})();
