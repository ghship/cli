const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto');
const {spawnSync}=require('node:child_process');
const [sourceArg,destArg]=process.argv.slice(2);
if(!sourceArg||!destArg||process.argv.length!==4)throw Error('usage: node unpack.cjs <bundle-directory> <new-output-directory>');
const source=path.resolve(sourceArg),dest=path.resolve(destArg);
const manifest=JSON.parse(fs.readFileSync(path.join(source,'manifest.json')));
const targets=['darwin-x64','darwin-arm64','linux-x64','linux-arm64','win32-x64','win32-arm64'];
if(!/^\d+\.\d+\.\d+$/.test(manifest.version)||manifest.packages.length!==7)throw Error('invalid package set');
const wanted=new Set(['@ghship/cli',...targets.map(t=>'@ghship/cli-'+t)]);
fs.mkdirSync(dest);
for(const p of manifest.packages){
 if(!wanted.delete(p.name)||p.filename!==p.filename.split('/').pop()||!/^ghship-cli-[a-z0-9.-]+\.tgz$/.test(p.filename))throw Error('invalid bundle member');
 const target=p.name==='@ghship/cli'?null:p.name.slice('@ghship/cli-'.length);
 if(p.target!==target)throw Error('invalid target');
 const file=path.join(source,p.filename),data=fs.readFileSync(file);
 if(crypto.createHash('sha256').update(data).digest('hex')!==p.sha256||'sha512-'+crypto.createHash('sha512').update(data).digest('base64')!==p.integrity)throw Error('candidate hash mismatch');
 const out=target?path.join(dest,'package-platforms',target):path.join(dest,'package');fs.mkdirSync(out,{recursive:true});
 // Pass the archive on stdin: GNU tar treats a Windows drive letter in an
 // archive filename as a remote host, while both GNU and BSD tar accept stdin.
 const result=spawnSync('tar',['-xzf','-','--strip-components=1'],{cwd:out,input:data,encoding:'utf8'});
 if(result.status!==0)throw Error(`cannot extract ${p.filename}: ${result.stderr}`);
 const meta=JSON.parse(fs.readFileSync(path.join(out,'package.json')));
 if(meta.name!==p.name||meta.version!==manifest.version)throw Error('candidate metadata mismatch');
}
if(wanted.size)throw Error('platform package missing');
console.log(`Verified and extracted seven ${manifest.version} candidates`);
