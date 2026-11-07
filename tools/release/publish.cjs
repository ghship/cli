// Publish exactly the verified CI bundle, in dependency order. No token is read
// or printed here; npm obtains short-lived credentials from its trusted publisher.
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),os=require('node:os');
const {spawnSync}=require('node:child_process');
const {singlePack}=require('../../npm/run.cjs');
const {guardFromEnvironment,validReleaseTag}=require('./release_guard.cjs');
const dir=path.resolve(process.argv[2]||'');
if(process.argv.length!==3)throw Error('usage: node publish.cjs <verified-bundle-directory>');
const manifest=JSON.parse(fs.readFileSync(path.join(dir,'manifest.json')));
const targets=['darwin-x64','darwin-arm64','linux-x64','linux-arm64','win32-x64','win32-arm64'];
const expected=new Set(['@ghship/cli',...targets.map(t=>'@ghship/cli-'+t)]);
if(!validReleaseTag('v'+manifest.version)||manifest.packages.length!==7)throw Error('invalid release manifest');
for(const p of manifest.packages){
 if(!expected.delete(p.name)||p.filename!==path.basename(p.filename)||!/^ghship-cli-[a-z0-9.-]+\.tgz$/.test(p.filename))throw Error('invalid package set');
 const data=fs.readFileSync(path.join(dir,p.filename));
 if(crypto.createHash('sha256').update(data).digest('hex')!==p.sha256||'sha512-'+crypto.createHash('sha512').update(data).digest('base64')!==p.integrity)throw Error(`candidate digest mismatch: ${p.name}`);
}
if(expected.size)throw Error('incomplete package set');
const registry='https://registry.npmjs.org/';
function npm(args) {
 const env={...process.env};delete env.GH_TOKEN;delete env.GITHUB_TOKEN;
 const r=spawnSync('npm',[...args,'--registry',registry],{encoding:'utf8',env});
 if(r.error)throw r.error;
 return r;
}
function viewString(output,field){
 let value=JSON.parse(output);
 if(Array.isArray(value)){
  if(value.length!==1)throw Error(`Ambiguous npm ${field} result`);
  value=value[0];
 }
 if(typeof value!=='string'||value.length===0)throw Error(`Invalid npm ${field} result`);
 return value;
}
function versionIntegrity(p){
 const r=npm(['view',`${p.name}@${manifest.version}`,'dist.integrity','--json','--prefer-online']);
 if(r.status===0){const actual=viewString(r.stdout,'integrity');if(actual!==p.integrity)throw Error(`published content differs: ${p.name}`);return true;}
 let error;try{error=JSON.parse(r.stdout).error;}catch(_){}
 if(error?.code==='E404')return false;
 throw Error(`Cannot verify ${p.name}: ${r.stderr.trim()}`);
}
function latest(){
 const r=npm(['view','@ghship/cli','dist-tags.latest','--json','--prefer-online']);
 if(r.status!==0)throw Error(`Cannot check latest: ${r.stderr.trim()}`);
 return viewString(r.stdout,'latest');
}
function compare(a,b){
 const x=a.split('.').map(Number),y=b.split('.').map(Number);
 for(let i=0;i<3;i++){if(x[i]!==y[i])return x[i]-y[i];}return 0;
}
function protectLatest(){const current=latest();if(!validReleaseTag('v'+current)||compare(current,manifest.version)>0)throw Error(`Refusing to move latest backwards from ${current}`);}
function verifyDownload(p){
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'ghship-release-verify-'));
 try {
  // A fresh isolated cache requires npm to fetch the published tarball rather
  // than accepting metadata or a cached copy as proof of availability.
  const r=npm(['pack',`${p.name}@${manifest.version}`,'--json','--ignore-scripts','--prefer-online','--cache',path.join(temp,'cache'),'--pack-destination',temp]);
  if(r.status!==0){
   let error;try{error=JSON.parse(r.stdout).error;}catch(_){}
   // npm can expose version metadata before its tarball is available. Pack
   // reports either E404 or ETARGET during this processing window.
   if(error?.code==='E404'||error?.code==='ETARGET')return false;
   throw Error(`Cannot download ${p.name}@${manifest.version}: ${r.stderr.trim()}`);
  }
  const record=singlePack(JSON.parse(r.stdout),p.name);
  if(record.version!==manifest.version||record.filename!==p.filename)throw Error(`Unexpected download metadata: ${p.name}`);
  const data=fs.readFileSync(path.join(temp,p.filename));
  if(crypto.createHash('sha256').update(data).digest('hex')!==p.sha256||'sha512-'+crypto.createHash('sha512').update(data).digest('base64')!==p.integrity)throw Error(`Downloaded content differs: ${p.name}`);
  return true;
 } finally {fs.rmSync(temp,{recursive:true,force:true});}
}
async function waitAvailable(p){
 for(let i=0;i<80;i++){
  if(versionIntegrity(p)&&verifyDownload(p))return;
  await new Promise(resolve=>setTimeout(resolve,15000));
 }
 throw Error(`${p.name}@${manifest.version} not yet available; inspect npm before retrying`);
}
async function publish(p){
 if(versionIntegrity(p)){console.log(`${p.name}@${manifest.version}: already published with matching integrity`);return;}
 console.log(`Publishing ${p.name}@${manifest.version}`);
 const result=npm(['publish',path.join(dir,p.filename),'--access','public','--ignore-scripts','--tag','ghship-candidate']);
 if(result.stdout)process.stdout.write(result.stdout);
 if(result.stderr)process.stderr.write(result.stderr);
 if(result.status!==0)throw Error(`Publish failed for ${p.name}; inspect npm before retrying`);
}
(async()=>{
 protectLatest();
 const platforms=targets.map(target=>manifest.packages.find(p=>p.name==='@ghship/cli-'+target));
 for(const p of platforms){guardFromEnvironment(manifest);await publish(p);}
 for(const p of platforms)await waitAvailable(p);
 guardFromEnvironment(manifest);
 protectLatest();
 const main=manifest.packages.find(p=>p.name==='@ghship/cli');
 await publish(main);
 await waitAvailable(main);
 guardFromEnvironment(manifest);
 const current=latest();
 if(current!==manifest.version){
  protectLatest();
  guardFromEnvironment(manifest);
  const r=npm(['dist-tag','add',`${main.name}@${manifest.version}`,'latest']);
  if(r.status!==0)throw Error(`Cannot update latest: ${r.stderr.trim()}`);
  let verified=false;
  for(let i=0;i<12;i++){
   guardFromEnvironment(manifest);
   const observed=latest();
   if(observed===manifest.version){verified=true;break;}
   if(observed!==current)throw Error(`Unexpected latest during readback: ${observed}`);
   if(i<11)await new Promise(resolve=>setTimeout(resolve,5000));
  }
  if(!verified)throw Error('latest tag did not update within the verification window');
 }
 console.log(`Published and verified ${main.name}@${manifest.version} as latest`);
})().catch(error=>{console.error(error.message);process.exitCode=1});
