const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');

test('candidate archives work when the source path contains a colon', {skip:process.platform==='win32'}, t=>{
 const tmp=fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()),'ghship-unpack-'));
 t.after(()=>fs.rmSync(tmp,{recursive:true,force:true}));
 const source=path.join(tmp,'registry:cache'),fixture=path.join(tmp,'fixture');fs.mkdirSync(source);fs.mkdirSync(fixture);fs.mkdirSync(path.join(fixture,'package'));
 const targets=['darwin-x64','darwin-arm64','linux-x64','linux-arm64','win32-x64','win32-arm64'];
 const packages=[null,...targets].map((target,i)=>{
  const name=target?'@ghship/cli-'+target:'@ghship/cli';
  fs.writeFileSync(path.join(fixture,'package','package.json'),JSON.stringify({name,version:'1.0.3'}));
  const staging=path.join(tmp,`staging-${i}.tgz`),filename=`ghship-cli-fixture-${i}.tgz`;
  const packed=spawnSync('tar',['-czf',staging,'-C',fixture,'package'],{encoding:'utf8'});assert.equal(packed.status,0,packed.stderr);
  const data=fs.readFileSync(staging);fs.writeFileSync(path.join(source,filename),data);
  return {name,target,filename,sha256:crypto.createHash('sha256').update(data).digest('hex'),integrity:'sha512-'+crypto.createHash('sha512').update(data).digest('base64')};
 });
 fs.writeFileSync(path.join(source,'manifest.json'),JSON.stringify({version:'1.0.3',packages}));
 const bin=path.join(tmp,'bin');fs.mkdirSync(bin);
 fs.writeFileSync(path.join(bin,'tar'), '#!/bin/sh\nfor arg in "$@"; do if [ "$arg" = "-C" ]; then echo "native destination path passed to POSIX tar" >&2; exit 1; fi; done\nexec /usr/bin/tar "$@"\n',{mode:0o755});
 const env={...process.env,PATH:bin+path.delimiter+process.env.PATH};
 const result=spawnSync(process.execPath,[path.join(__dirname,'unpack.cjs'),source,path.join(tmp,'extracted')],{encoding:'utf8',env});
 assert.equal(result.status,0,result.stderr);
});
