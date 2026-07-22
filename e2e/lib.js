// Shared helpers for the browser suites.

class Checks {
  constructor(name) {
    this.name = name;
    this.pass = 0;
    this.fail = 0;
  }

  check(label, ok, detail) {
    if (ok) {
      console.log(`  PASS  ${label}`);
      this.pass++;
    } else {
      console.log(`  FAIL  ${label}${detail ? ' — ' + detail : ''}`);
      this.fail++;
    }
    return ok;
  }

  section(title) {
    console.log(`\n[${this.name}] ${title}`);
  }

  summary() {
    console.log(`\n---- ${this.name}: ${this.pass} passed, ${this.fail} failed ----`);
    return this.fail === 0;
  }
}

// waitFor polls until predicate returns truthy or the deadline passes. Used
// instead of a fixed sleep so the suites stay fast when things are quick and
// still tolerate a slow CI runner.
async function waitFor(predicate, { timeout = 12000, interval = 200 } = {}) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await predicate()) return true;
    await new Promise(r => setTimeout(r, interval));
  }
  return false;
}

module.exports = { Checks, waitFor };
