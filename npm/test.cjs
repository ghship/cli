const { test } = require('node:test');
const assert = require('node:assert/strict');
const { binaryPath } = require('./run.cjs');
test('six target selection paths', () => {
  for (const os of ['darwin', 'linux', 'win32']) for (const cpu of ['x64','arm64'])
    assert.match(binaryPath(os, cpu), os === 'win32' ? /ghship\.exe$/ : /ghship$/);
  assert.throws(() => binaryPath('freebsd','x64'), /Unsupported/);
  assert.throws(() => binaryPath('linux','ia32'), /Unsupported/);
});
