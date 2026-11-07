const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const script = path.join(__dirname, 'publish.cjs');
const targets = ['darwin-x64','darwin-arm64','linux-x64','linux-arm64','win32-x64','win32-arm64'];
function fixture(t, options={}) {
 const dir=fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()),'ghship-publish-test-'));
 t.after(()=>fs.rmSync(dir,{recursive:true,force:true}));
 const bundle=path.join(dir,'bundle'), bin=path.join(dir,'bin');fs.mkdirSync(bundle);fs.mkdirSync(bin);
 const packages=['@ghship/cli',...targets.map(target=>'@ghship/cli-'+target)].map((name,i)=>{
  const filename=`ghship-cli-fixture-${i}.tgz`, data=Buffer.from(name);
  fs.writeFileSync(path.join(bundle,filename),data);
  return {name,filename,sha256:crypto.createHash('sha256').update(data).digest('hex'),integrity:'sha512-'+crypto.createHash('sha512').update(data).digest('base64')};
 });
 const manifest={version:'1.0.3',packages};fs.writeFileSync(path.join(bundle,'manifest.json'),JSON.stringify(manifest));
 const state=path.join(dir,'state.json');fs.writeFileSync(state,JSON.stringify({latest:options.latest||'1.0.2',published:options.published||{},downloadFailure:options.downloadFailure||false,transientPackErrors:options.transientPackErrors||[],corruptDownload:options.corruptDownload||false,arrayViews:options.arrayViews||false,objectPacks:options.objectPacks||false,multipleViews:options.multipleViews||false,emptyLatest:options.emptyLatest||false,multipleIntegrity:options.multipleIntegrity||false,calls:[]}));
 const mock=`#!/usr/bin/env node
const fs=require('node:fs'),path=require('node:path');
const stateFile=process.env.TASK_RELEASE_STATE, manifest=JSON.parse(fs.readFileSync(process.env.TASK_RELEASE_MANIFEST));
const state=JSON.parse(fs.readFileSync(stateFile));const args=process.argv.slice(2);state.calls.push(args);
function done(code,text){fs.writeFileSync(stateFile,JSON.stringify(state));if(text)process.stdout.write(text);process.exit(code)}
function view(value){return JSON.stringify(state.multipleViews?[value,value]:state.arrayViews?[value]:value)}
if(args[0]==='view'&&args[1]==='@ghship/cli')done(0,state.emptyLatest?'[]':view(state.latest));
if(args[0]==='view'){const key=args[1];if(state.published[key])done(0,state.multipleIntegrity?JSON.stringify([state.published[key],state.published[key]]):view(state.published[key]));done(1,JSON.stringify({error:{code:'E404'}}));}
if(args[0]==='publish'){const p=manifest.packages.find(p=>p.filename===path.basename(args[1]));if(!p)done(2,'');state.published[p.name+'@'+manifest.version]=p.integrity;if(p.name==='@ghship/cli')state.latest=manifest.version;done(0,'ok');}
if(args[0]==='pack'){if(state.downloadFailure)done(1,JSON.stringify({error:{code:'E403'}}));if(state.transientPackErrors.length)done(1,JSON.stringify({error:{code:state.transientPackErrors.shift()}}));const p=manifest.packages.find(p=>args[1]===p.name+'@'+manifest.version);if(!p)done(2,'');const dest=args[args.indexOf('--pack-destination')+1];const file=path.join(dest,p.filename);fs.copyFileSync(path.join(path.dirname(process.env.TASK_RELEASE_MANIFEST),p.filename),file);if(state.corruptDownload)fs.appendFileSync(file,'modified');const record={name:p.name,version:manifest.version,filename:p.filename};done(0,JSON.stringify(state.objectPacks?{[p.name]:record}:[record]));}
if(args[0]==='dist-tag'){state.latest=manifest.version;done(0,'');}
done(3,'');`;
 fs.writeFileSync(path.join(bin,'npm'),mock,{mode:0o755});
 const timer=path.join(dir,'timer.cjs');fs.writeFileSync(timer,'const real=setTimeout;global.setTimeout=(fn,ms,...args)=>real(fn,0,...args);');
 const env={...process.env, PATH:bin+path.delimiter+process.env.PATH,NODE_OPTIONS:`--require=${timer}`,TASK_RELEASE_STATE:state,TASK_RELEASE_MANIFEST:path.join(bundle,'manifest.json')};
 return {manifest,state,run:()=>spawnSync(process.execPath,[script,bundle],{env,encoding:'utf8'}), read:()=>JSON.parse(fs.readFileSync(state))};
}

test('publishes the six verified platforms before the main latest package', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t),r=f.run();assert.equal(r.status,0,r.stderr);
 const calls=f.read().calls.filter(a=>a[0]==='publish');
 assert.equal(calls.length,7);
 assert.equal(path.basename(calls[6][1]),f.manifest.packages[0].filename);
 assert.deepEqual(calls.slice(0,6).map(c=>path.basename(c[1])).sort(),f.manifest.packages.slice(1).map(p=>p.filename).sort());
 const beforeMain=f.read().calls.slice(0,f.read().calls.findIndex(a=>a[0]==='publish'&&path.basename(a[1])===f.manifest.packages[0].filename));
 assert.equal(beforeMain.filter(a=>a[0]==='pack').length,6);
 assert.equal(f.read().latest,'1.0.3');
 const again=f.run();assert.equal(again.status,0,again.stderr);assert.equal(f.read().calls.filter(a=>a[0]==='publish').length,7);
});

test('refuses to move latest backwards', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t,{latest:'1.0.4'}),r=f.run();assert.notEqual(r.status,0);assert.match(r.stderr,/move latest backwards/);assert.equal(f.read().calls.filter(a=>a[0]==='publish').length,0);
});

test('refuses an already published package with different integrity', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t,{published:{'@ghship/cli-darwin-x64@1.0.3':'sha512-not-the-candidate'}}),r=f.run();
 assert.notEqual(r.status,0);assert.match(r.stderr,/published content differs/);assert.equal(f.read().calls.filter(a=>a[0]==='publish').length,0);
});

test('never publishes main when platform metadata exists but downloading fails', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t,{downloadFailure:true}),r=f.run();
 assert.notEqual(r.status,0);assert.match(r.stderr,/download/);
 const main=f.manifest.packages[0].filename;
 assert.equal(f.read().calls.filter(a=>a[0]==='publish'&&path.basename(a[1])===main).length,0);
});

test('waits for scan-delayed package downloads before publishing main', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t,{transientPackErrors:['ETARGET','E404']}),r=f.run();
 assert.equal(r.status,0,r.stderr);
 const calls=f.read().calls, main=f.manifest.packages[0].filename;
 const beforeMain=calls.slice(0,calls.findIndex(a=>a[0]==='publish'&&path.basename(a[1])===main));
 assert.equal(beforeMain.filter(a=>a[0]==='pack').length,8);
});

test('stops after bounded retries if a package never becomes downloadable', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t,{transientPackErrors:Array(80).fill('ETARGET')}),r=f.run();
 assert.notEqual(r.status,0);assert.match(r.stderr,/not yet available/);
 const calls=f.read().calls,main=f.manifest.packages[0].filename;
 assert.equal(calls.filter(a=>a[0]==='pack').length,80);
 assert.equal(calls.filter(a=>a[0]==='publish'&&path.basename(a[1])===main).length,0);
});

test('never publishes main when the downloaded platform package differs', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t,{corruptDownload:true}),r=f.run();
 assert.notEqual(r.status,0);assert.match(r.stderr,/Downloaded content differs/);
 const main=f.manifest.packages[0].filename;
 assert.equal(f.read().calls.filter(a=>a[0]==='publish'&&path.basename(a[1])===main).length,0);
});

test('npm 12 single-element view results are accepted for latest and integrity', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t,{arrayViews:true,objectPacks:true}),r=f.run();assert.equal(r.status,0,r.stderr);
 assert.equal(f.read().latest,'1.0.3');
 const again=f.run();assert.equal(again.status,0,again.stderr);
});

test('ambiguous npm view results never publish packages', {skip:process.platform==='win32'}, t=>{
 const f=fixture(t,{multipleViews:true}),r=f.run();assert.notEqual(r.status,0);
 assert.equal(f.read().calls.filter(a=>a[0]==='publish').length,0);
});

test('empty latest and ambiguous integrity fail before any package publication', {skip:process.platform==='win32'}, t=>{
 const a=fixture(t,{emptyLatest:true}),ar=a.run();assert.notEqual(ar.status,0);assert.equal(a.read().calls.filter(c=>c[0]==='publish').length,0);
 const b=fixture(t,{multipleIntegrity:true});const state=b.read();state.published['@ghship/cli-darwin-x64@1.0.3']=b.manifest.packages[1].integrity;fs.writeFileSync(b.state,JSON.stringify(state));
 const br=b.run();assert.notEqual(br.status,0);assert.match(br.stderr,/Ambiguous npm integrity/);assert.equal(b.read().calls.filter(c=>c[0]==='publish').length,0);
});
