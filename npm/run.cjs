#!/usr/bin/env node
const path = require('node:path');
const fs = require('node:fs');
const os = require('node:os');
const crypto = require('node:crypto');
const { spawn, spawnSync } = require('node:child_process');
function binaryPath(platform = process.platform, arch = process.arch) {
 const os = {darwin:'darwin', linux:'linux', win32:'windows'}[platform];
 const cpu = {x64:'amd64', arm64:'arm64'}[arch];
 if (!os || !cpu) throw new Error(`Unsupported platform: ${platform}/${arch}`);
 return path.join(__dirname, 'bin', `${os}-${cpu}`, os === 'windows' ? 'ghship.exe' : 'ghship');
}
function npmCommand() {
 if (process.env.npm_execpath && fs.existsSync(process.env.npm_execpath)) return [process.execPath, [process.env.npm_execpath]];
 const suffixes = process.platform === 'win32' ? ['npm.cmd'] : ['npm'];
 for (const dir of (process.env.PATH || '').split(path.delimiter)) for (const name of suffixes) {
  const candidate = path.join(dir, name); if (!fs.existsSync(candidate)) continue;
  const real = fs.realpathSync(candidate);
  if (process.platform !== 'win32') return [process.execPath, [real]];
  const cli = path.join(dir, 'node_modules', 'npm', 'bin', 'npm-cli.js');
  if (fs.existsSync(cli)) return [process.execPath, [cli]];
 }
 throw new Error('npm is unavailable. Install Node/npm: https://nodejs.org/en/download');
}
function npm(args, capture = false) {
 const [command, base] = npmCommand();
 const result = spawnSync(command, [...base, ...args], {stdio:capture ? ['ignore','pipe','pipe'] : 'inherit', encoding:'utf8'});
 const absentList = args[0] === 'list' && result.status !== 0 && (() => {
  try { const value=JSON.parse(result.stdout); return value.error?.code === 'ENOENT' || (result.status === 1 && !value.error); } catch (_) { return false; }
 })();
 if (result.error || (result.status !== 0 && !absentList)) throw new Error(`npm ${args[0]} failed; retry the same fixed-version command`);
 return capture ? (absentList ? '{"dependencies":{}}' : result.stdout) : '';
}
function go(executable,args,capture=false,interactive=false) {
 const result=spawnSync(executable,args,{stdio:capture?[interactive?'inherit':'ignore','pipe',interactive?'inherit':'pipe']:'inherit',encoding:'utf8'});
 if(result.error) throw new Error('Cannot start the packaged Go CLI');
 if (capture && result.status !== 0 && result.stderr) process.stderr.write(result.stderr);
 return {code:result.status===null?1:result.status,stdout:result.stdout};
}
function hashFile(file) { return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex'); }
function noLinks(file) {
 for(let current=file;;current=path.dirname(current)) {
  try {if(fs.lstatSync(current).isSymbolicLink())throw new Error('Installation path contains a symlink; preserved');}
  catch(error){if(error.code!=='ENOENT')throw error;}
  if(path.dirname(current)===current)break;
 }
}
function packageReceipt(root) {
 const base=process.platform==='win32' ? path.join(process.env.APPDATA||path.join(os.homedir(),'AppData','Roaming'),'ghship') : path.join(os.homedir(),'.config','ghship');
 return path.join(base,`npm-${crypto.createHash('sha256').update(root).digest('hex')}.json`);
}
function observePackage(root,meta,existing,replacingVerifiedReceipt=false) {
 const directory=path.join(root,meta.name); noLinks(directory);
 const metadata=JSON.parse(fs.readFileSync(path.join(directory,'package.json'),'utf8'));
 if(metadata.name!==meta.name||metadata.version!==existing.version||metadata.bin?.ghship!=='run.cjs')throw new Error('Unrecognized global package; preserved');
 const relative=path.relative(__dirname,binaryPath());
 const observed={root,name:meta.name,version:metadata.version,launcher:hashFile(path.join(directory,'run.cjs')),binary:hashFile(path.join(directory,relative))};
 const current=observed.version===meta.version&&observed.launcher===hashFile(path.join(__dirname,'run.cjs'))&&observed.binary===hashFile(binaryPath());
 const receipt=packageReceipt(root);noLinks(receipt);
 let recorded=null;try{recorded=JSON.parse(fs.readFileSync(receipt,'utf8'));}catch(error){if(error.code!=='ENOENT')throw new Error('Invalid installation receipt; preserved');}
 if(recorded&&!replacingVerifiedReceipt&&!Object.keys(observed).every(key=>recorded[key]===observed[key]))throw new Error('Installation receipt does not match the package; preserved');
 const managed=current||(recorded&&Object.keys(observed).every(key=>recorded[key]===observed[key]));
 if(!managed)throw new Error('Global package ownership/content is unverified; preserved. Inspect npm list -g @ghship/cli before changing it');
 return {observed,current};
}
function saveReceipt(root,observed) {
 const file=packageReceipt(root);noLinks(file);fs.mkdirSync(path.dirname(file),{recursive:true,mode:0o700});
 const stage=`${file}.${process.pid}.tmp`;const descriptor=fs.openSync(stage,'wx',0o600);
 try{fs.writeFileSync(descriptor,JSON.stringify(observed));fs.fsyncSync(descriptor);}finally{fs.closeSync(descriptor);}
 try{fs.renameSync(stage,file);}finally{if(fs.existsSync(stage))fs.unlinkSync(stage);}
}
function lifecycle(executable,args) {
 const action=args[0], meta=require('./package.json'); let source=`${meta.name}@${meta.version}`;
 const passed=args.slice(1); const sourceIndex=passed.indexOf('--package-source');
 if(sourceIndex>=0) {if(!passed[sourceIndex+1])throw new Error('--package-source requires a local tarball');source=path.resolve(passed[sourceIndex+1]);passed.splice(sourceIndex,2);
  const packed=JSON.parse(npm(['pack',source,'--dry-run','--json'],true));if(packed.length!==1||packed[0].name!==meta.name||packed[0].version!==meta.version)throw new Error('Local package source must match this exact package and version');
 }
 const lifecycleArgs=[];const initArgs=[];let userOnly=false;
 const initValues=new Set(['--project','--repo','--author','--reviewer','--admin','--acceptance-pr']);
 const initSwitches=new Set(['--configure','--non-interactive','--confirm-auto-close']);
 for(let i=0;i<passed.length;i++){
  const arg=passed[i];
  if(arg==='--user-only'){userOnly=true;continue;}
  if(arg==='--dir'){if(!passed[i+1])throw new Error('--dir requires a path');lifecycleArgs.push(arg,passed[++i]);}
  else if(arg==='--json'||initSwitches.has(arg)){initArgs.push(arg);}
  else if(initValues.has(arg)){if(!passed[i+1])throw new Error(`${arg} requires a value`);initArgs.push(arg,passed[++i]);}
  else throw new Error(`Unknown lifecycle option: ${arg}; use ghship help install`);
 }
 const preflight=go(executable,[action,'--skill-only','--check',...lifecycleArgs,'--json'],true);if(preflight.code!==0)return preflight.code;
 const installed=JSON.parse(npm(['list','-g',meta.name,'--depth=0','--json'],true));
 const existing=installed.dependencies && installed.dependencies[meta.name];
 const root=path.resolve(npm(['root','-g'],true).trim());
 const receipt=packageReceipt(root);noLinks(receipt);
 if(!existing&&action!=='uninstall'&&fs.existsSync(receipt))throw new Error('Installation receipt exists without its package; preserved');
 if(existing && (existing.invalid||existing.extraneous))throw new Error('Global package ownership is inconsistent; inspect npm list -g @ghship/cli');
 const observation=existing ? observePackage(root,meta,existing) : null;
 if(action==='uninstall') {
  const removal=go(executable,['uninstall','--skill-only',...lifecycleArgs,'--json'],true);if(removal.code!==0)return removal.code;
  if(existing)npm(['uninstall','-g',meta.name],true);
  const receipt=packageReceipt(root);noLinks(receipt);if(existing&&fs.existsSync(receipt))fs.unlinkSync(receipt);
  console.log(JSON.stringify({userInstalled:false,projectConfigurationsPreserved:true,credentialsPreserved:true}));return 0;
 }
 const skill=go(executable,['install','--skill-only',...lifecycleArgs,'--json'],true);if(skill.code!==0)return skill.code;
 try{if(!observation||!observation.current)npm(['install','-g',source],true);}catch(error){console.error(`Skill is ${meta.version}; CLI installation incomplete. Retry npx ${meta.name}@${meta.version} ${action}`);throw error;}
 const verified=JSON.parse(npm(['list','-g',meta.name,'--depth=0','--json'],true));
 if(!verified.dependencies||verified.dependencies[meta.name]?.version!==meta.version)throw new Error('Installed CLI version does not match skill; installation incomplete');
 const persisted=observePackage(root,meta,verified.dependencies[meta.name],!!observation);if(!persisted.current)throw new Error('Persistent CLI content does not match this package');
 const runtimeVersion=go(path.join(root,meta.name,path.relative(__dirname,executable)),['version'],true);
 if(runtimeVersion.code!==0||runtimeVersion.stdout.trim()!==meta.version)throw new Error('Persistent CLI cannot run the expected version');
 saveReceipt(root,persisted.observed);
 if(action==='update'||userOnly){console.log(JSON.stringify({userInstalled:true,version:meta.version,projectInitialized:false}));return 0;}
 const init=go(executable,['init',...initArgs,'--json'],true,true);
 let result;try{result=JSON.parse(init.stdout);}catch(_){result={ready:false,missing:['Initialization failed before producing a result'],next:['ghship init --json']};}
 result.userInstalled=true;result.version=meta.version;console.log(JSON.stringify(result));return init.code;
}
module.exports = {binaryPath};
if(require.main===module) {
 try {
  const executable=binaryPath();if(!fs.existsSync(executable))throw new Error('Package missing Go binary; reinstall the package');
  const args=process.argv.slice(2);const action=args[0];
  if(['install','update','uninstall'].includes(action)&&!args.includes('--standalone')&&!args.includes('--skill-only')&&!args.includes('--help')){
   if(action!=='install'&&process.env.npm_command!=='exec') {console.error(`Run npx @ghship/cli@latest ${action} from an external npm entry`);process.exitCode=4;}
   else process.exitCode=lifecycle(executable,args);
  } else {
   const child=spawn(executable,args,{stdio:'inherit'});child.on('error',()=>{console.error('Cannot start Go CLI');process.exitCode=1});child.on('exit',(code,signal)=>{if(signal)process.kill(process.pid,signal);else process.exitCode=code===null?1:code});
  }
 }catch(error){console.error(error.message);process.exitCode=1;}
}
