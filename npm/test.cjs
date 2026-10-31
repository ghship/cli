const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { binaryPath } = require('./run.cjs');
test('six target selection paths', () => {
  for (const os of ['darwin', 'linux', 'win32']) for (const cpu of ['x64','arm64'])
    assert.match(binaryPath(os, cpu), os === 'win32' ? /ghship\.exe$/ : /ghship$/);
  assert.throws(() => binaryPath('freebsd','x64'), /Unsupported/);
  assert.throws(() => binaryPath('linux','ia32'), /Unsupported/);
});

function assertLicenseNotices(readme, projectLicense) {
  assert.ok(readme.includes(projectLicense), 'project MIT license must be included');
  const dependency = readme.indexOf('Copyright 2009 The Go Authors.');
  const runtimeIntro = readme.indexOf('The bundled binaries include the Go runtime and standard library,', dependency);
  const runtime = readme.indexOf('Copyright (c) 2009 The Go Authors. All rights reserved.', runtimeIntro);
  assert.ok(dependency >= 0 && runtimeIntro > dependency && runtime > runtimeIntro, 'Go license notices must be present');
  const digest = text => crypto.createHash('sha256').update(text.replace(/\r\n/g, '\n')
    .split('\n').map(line => line.trim()).filter(Boolean).join('\n')).digest('hex');
  // Normalized text hashes of the official Go 1.23.12 and x/sys v0.29.0
  // license files; x/term v0.28.0 has the same text as x/sys.
  assert.equal(digest(readme.slice(dependency, runtimeIntro)), '53fe6e5c9fc50d0109a55ab6a0930717b93cf564950afd5bbf095d1a67267e64', 'complete x/sys and x/term license');
  assert.equal(digest(readme.slice(runtime)), 'f2d913c40d0a6eea0969b36dc618b015614d2308cb70a263b0a9647261f762cc', 'complete Go runtime license');
}

const readme = fs.readFileSync(path.join(__dirname, 'README.md'), 'utf8');
const projectLicense = fs.readFileSync(path.join(__dirname, '..', 'LICENSE'), 'utf8').replace(/\r\n/g, '\n').trim();
test('distribution README contains project and Go binary license notices', () => {
  assertLicenseNotices(readme, projectLicense);
});

test('distribution notices reject missing license terms', () => {
  const runtime = readme.indexOf('Copyright (c) 2009 The Go Authors. All rights reserved.');
  const dependency = readme.indexOf('Copyright 2009 The Go Authors.');
  assert.ok(runtime > dependency && dependency >= 0);
  for (const [label, start, end, term] of [
    ['runtime disclaimer', runtime, readme.length, 'THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS'],
    ['runtime condition', runtime, readme.length, 'notice, this list of conditions and the following disclaimer.'],
    ['dependency disclaimer', dependency, runtime, 'THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS'],
    ['dependency condition', dependency, runtime, 'notice, this list of conditions and the following disclaimer.'],
  ]) {
    const segment = readme.slice(start, end);
    assert.ok(segment.includes(term), `${label} fixture exists`);
    const corrupted = readme.slice(0, start) + segment.replace(term, '') + readme.slice(end);
    assert.throws(() => assertLicenseNotices(corrupted, projectLicense), undefined, label);
  }
});
