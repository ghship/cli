const { test } = require('node:test');
const assert = require('node:assert/strict');
const { binaryPath } = require('./run.cjs');
test('six target selection paths', () => {
  for (const os of ['darwin', 'linux', 'win32']) for (const cpu of ['x64','arm64'])
    assert.match(binaryPath(os, cpu), os === 'win32' ? /ghship\.exe$/ : /ghship$/);
  assert.throws(() => binaryPath('freebsd','x64'), /Unsupported/);
  assert.throws(() => binaryPath('linux','ia32'), /Unsupported/);
});

test('distribution README contains project and Go binary license notices', () => {
  const fs = require('node:fs');
  const path = require('node:path');
  const readme = fs.readFileSync(path.join(__dirname, 'README.md'), 'utf8');
  const projectLicense = fs.readFileSync(path.join(__dirname, '..', 'LICENSE'), 'utf8').replace(/\r\n/g, '\n').trim();
  assert.ok(readme.includes(projectLicense), 'project MIT license must be included');
  assert.match(readme, /Copyright \(c\) 2009 The Go Authors\. All rights reserved\./);
  assert.match(readme, /Neither the name of Google Inc\. nor the names of its/);
  assert.match(readme, /Copyright 2009 The Go Authors\./);
  assert.match(readme, /Neither the name of Google LLC nor the names of its/);
});
