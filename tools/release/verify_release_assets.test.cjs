'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto');
const {verifyDownloaded}=require('./verify_release_assets.cjs');
test('actual downloaded release packages must match the candidate bytes and complete set',t=>{
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'ghship-release-download-test-'));t.after(()=>fs.rmSync(dir,{recursive:true,force:true}));
 const data=Buffer.from('candidate'),p={name:'@ghship/cli',filename:'ghship-cli-1.0.8.tgz',sha256:crypto.createHash('sha256').update(data).digest('hex'),integrity:'sha512-'+crypto.createHash('sha512').update(data).digest('base64')},manifest={packages:[p]};
 fs.writeFileSync(path.join(dir,p.filename),data);assert.doesNotThrow(()=>verifyDownloaded(dir,manifest));
 fs.writeFileSync(path.join(dir,p.filename),'different');assert.throws(()=>verifyDownloaded(dir,manifest),/differs/);
 fs.rmSync(path.join(dir,p.filename));assert.throws(()=>verifyDownloaded(dir,manifest),/set differs/);
});

test('release download is bracketed by latest checks and rejects changed bytes or latest', {skip:process.platform==='win32'},t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'ghship-release-fetch-test-'));t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const bin=path.join(root,'bin'),candidate=path.join(root,'candidate');fs.mkdirSync(bin);fs.mkdirSync(candidate);
 const data=Buffer.from('candidate'),p={name:'@ghship/cli',filename:'ghship-cli-1.0.8.tgz',sha256:crypto.createHash('sha256').update(data).digest('hex'),integrity:'sha512-'+crypto.createHash('sha512').update(data).digest('base64')};
 fs.writeFileSync(path.join(candidate,'manifest.json'),JSON.stringify({version:'1.0.8',packages:[p]}));fs.writeFileSync(path.join(root,'guard.json'),JSON.stringify({id:42,tag:'v1.0.8',sha:'a'.repeat(40)}));fs.writeFileSync(path.join(root,'content'),data);
 const mock=`#!/usr/bin/env node\nconst fs=require('fs'),path=require('path'),args=process.argv.slice(2),s=JSON.parse(fs.readFileSync(process.env.TASK_STATE)),q=args.at(-1);if(args[0]==='api'&&q.includes('/git/ref/tags/'))process.stdout.write(JSON.stringify({object:{type:'commit',sha:'a'.repeat(40)}}));else if(args[0]==='api'&&q.includes('/compare/'))process.stdout.write(JSON.stringify({status:'ahead',base_commit:{sha:'a'.repeat(40)}}));else if(args[0]==='api'){s.checks++;fs.writeFileSync(process.env.TASK_STATE,JSON.stringify(s));process.stdout.write(JSON.stringify({id:s.changeAfter&&s.checks>=s.changeAfter?43:42,tag_name:'v1.0.8',draft:false,prerelease:false,assets:[{name:'ghship-cli-1.0.8.tgz',state:'uploaded',digest:'sha256:'+s.sha}]}));}else if(args[0]==='release'&&args[1]==='download'){const dir=args[args.indexOf('--dir')+1];fs.writeFileSync(path.join(dir,'ghship-cli-1.0.8.tgz'),s.corrupt?'bad':fs.readFileSync(process.env.TASK_CONTENT));}else process.exit(2);`;
 fs.writeFileSync(path.join(bin,'gh'),mock,{mode:0o755});
 const state=path.join(root,'state.json'),env={...process.env,PATH:bin+path.delimiter+process.env.PATH,GITHUB_REPOSITORY:'ghship/cli',GHSHIP_RELEASE_GUARD:path.join(root,'guard.json'),TASK_STATE:state,TASK_CONTENT:path.join(root,'content'),TMPDIR:root};
 const run=opts=>{fs.writeFileSync(state,JSON.stringify({checks:0,sha:p.sha256,...opts}));return require('child_process').spawnSync(process.execPath,[path.join(__dirname,'verify_release_assets.cjs'),candidate],{env,encoding:'utf8'});};
 let r=run({});assert.equal(r.status,0,r.stderr);
 r=run({corrupt:true});assert.notEqual(r.status,0);assert.match(r.stderr,/differs/);
 r=run({changeAfter:2});assert.notEqual(r.status,0);assert.match(r.stderr,/not the current formal latest/);
 assert.equal(fs.readdirSync(root).filter(name=>name.startsWith('ghship-release-assets-')).length,0);
});
