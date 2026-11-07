'use strict';
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),crypto=require('node:crypto');
const {spawnSync}=require('node:child_process');
const {guardFromEnvironment}=require('./release_guard.cjs');
function verifyDownloaded(dir,manifest){
 const files=fs.readdirSync(dir).sort(),expected=manifest.packages.map(p=>p.filename).sort();
 if(files.length!==expected.length||files.some((n,i)=>n!==expected[i]))throw Error('Downloaded release package set differs from candidate');
 for(const p of manifest.packages){
  const data=fs.readFileSync(path.join(dir,p.filename));
  if(crypto.createHash('sha256').update(data).digest('hex')!==p.sha256||'sha512-'+crypto.createHash('sha512').update(data).digest('base64')!==p.integrity)throw Error(`Downloaded release package differs: ${p.name}`);
 }
}
if(require.main===module){
 if(process.argv.length!==3)throw Error('usage: node verify_release_assets.cjs <candidate-directory>');
 const manifest=JSON.parse(fs.readFileSync(path.join(process.argv[2],'manifest.json')));
 const guard=JSON.parse(fs.readFileSync(process.env.GHSHIP_RELEASE_GUARD));
 guardFromEnvironment(manifest);
 const tmp=fs.mkdtempSync(path.join(os.tmpdir(),'ghship-release-assets-'));
 try {
  const args=['release','download',guard.tag,'--repo',process.env.GITHUB_REPOSITORY,'--dir',tmp];
  for(const p of manifest.packages)args.push('--pattern',p.filename);
  const r=spawnSync('gh',args,{encoding:'utf8'});
  if(r.error||r.status!==0)throw Error(`Cannot download GitHub release packages: ${r.stderr?.trim()||r.error?.message||'unknown error'}`);
  verifyDownloaded(tmp,manifest);
  guardFromEnvironment(manifest);
  console.log(`Verified all seven downloadable GitHub release packages for ${guard.tag}`);
 } finally { fs.rmSync(tmp,{recursive:true,force:true}); }
}
module.exports={verifyDownloaded};
