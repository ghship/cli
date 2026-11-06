const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const { binaryPath, platformInfo } = require('./run.cjs');
const meta = require('./package.json');

test('the entry package pins exactly six platform packages and contains no binaries', () => {
  const expected = ['darwin-x64', 'darwin-arm64', 'linux-x64', 'linux-arm64', 'win32-x64', 'win32-arm64']
    .map(target => `@ghship/cli-${target}`).sort();
  assert.deepEqual(Object.keys(meta.optionalDependencies || {}).sort(), expected);
  for (const name of expected) assert.equal(meta.optionalDependencies[name], meta.version);
  assert.ok(!meta.files.includes('bin'));
});

test('missing, mismatched or modified platform binaries are rejected', t => {
  const tmp = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), 'ghship-platform-'));
  t.after(() => fs.rmSync(tmp, { recursive: true, force: true }));
  const main = path.join(tmp, 'node_modules', '@ghship', 'cli');
  const info = platformInfo();
  const platform = path.join(tmp, 'node_modules', info.name);
  fs.mkdirSync(main, { recursive: true });
  fs.writeFileSync(path.join(main, 'package.json'), JSON.stringify({ version: '1.0.3' }));
  assert.throws(() => binaryPath(process.platform, process.arch, main), /is missing; enable optional dependencies/);
  fs.mkdirSync(path.join(platform, 'bin'), { recursive: true });
  const manifest = { name: info.name, version: '1.0.3', os: [process.platform], cpu: [process.arch] };
  fs.writeFileSync(path.join(platform, 'package.json'), JSON.stringify(manifest));
  fs.writeFileSync(path.join(platform, 'bin', info.binary), 'fixture binary');
  fs.writeFileSync(path.join(main, 'checksums.json'), JSON.stringify({[info.target]: crypto.createHash('sha256').update('fixture binary').digest('hex')}));
  assert.equal(binaryPath(process.platform, process.arch, main), path.join(platform, 'bin', info.binary));
  manifest.version = '0.0.0';
  fs.writeFileSync(path.join(platform, 'package.json'), JSON.stringify(manifest));
  assert.throws(() => binaryPath(process.platform, process.arch, main), /does not match/);
  manifest.version = '1.0.3';
  fs.writeFileSync(path.join(platform, 'package.json'), JSON.stringify(manifest));
  fs.writeFileSync(path.join(platform, 'bin', info.binary), 'modified binary');
  assert.throws(() => binaryPath(process.platform, process.arch, main), /checksum mismatch/);
});
