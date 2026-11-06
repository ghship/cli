const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const [mainArg, outputArg] = process.argv.slice(2);
if (!mainArg || !outputArg || process.argv.length !== 4) throw Error('usage: node bundle.cjs <built-main-directory> <new-output-directory>');
const main = path.resolve(mainArg), output = path.resolve(outputArg);
const meta = JSON.parse(fs.readFileSync(path.join(main, 'package.json')));
const targets = ['darwin-x64','darwin-arm64','linux-x64','linux-arm64','win32-x64','win32-arm64'];
if (!/^\d+\.\d+\.\d+$/.test(meta.version)) throw Error('release requires an exact stable version');
if (meta.name !== '@ghship/cli' || meta.files.includes('bin')) throw Error('invalid main package');
const expected = Object.fromEntries(targets.map(target => [`@ghship/cli-${target}`, meta.version]));
if (JSON.stringify(Object.entries(meta.optionalDependencies||{}).sort()) !== JSON.stringify(Object.entries(expected).sort())) throw Error('platform dependency versions do not match');
const checks = JSON.parse(fs.readFileSync(path.join(main,'checksums.json')));
if (Object.keys(checks).sort().join() !== [...targets].sort().join()) throw Error('platform checksums incomplete');
const packages = [{name:meta.name, directory:main, target:null}];
for (const target of targets) {
 const directory=path.join(main+'-platforms',target), pm=JSON.parse(fs.readFileSync(path.join(directory,'package.json')));
 const [os,cpu]=target.split('-');
 if(pm.name!==`@ghship/cli-${target}`||pm.version!==meta.version||JSON.stringify(pm.os)!==JSON.stringify([os])||JSON.stringify(pm.cpu)!==JSON.stringify([cpu]))throw Error(`invalid ${target} metadata`);
 const binary=path.join(directory,'bin',os==='win32'?'ghship.exe':'ghship');
 if (crypto.createHash('sha256').update(fs.readFileSync(binary)).digest('hex')!==checks[target]) throw Error(`${target} binary checksum mismatch`);
 packages.push({name:pm.name,directory,target});
}
fs.mkdirSync(output);
const entries=[];
for (const pkg of packages) {
 const command=process.platform==='win32'?'npm.cmd':'npm';
 const result=spawnSync(command,['pack',pkg.directory,'--json','--ignore-scripts','--pack-destination',output],{encoding:'utf8',shell:process.platform==='win32'});
 if(result.status!==0)throw Error(`npm pack ${pkg.name} failed: ${result.stderr}`);
 const record=JSON.parse(result.stdout)[0];
 const allowed=pkg.target ? ['README.md','bin/'+(pkg.target.startsWith('win32-')?'ghship.exe':'ghship'),'package.json'] : ['README.md','checksums.json','package.json','run.cjs'];
 if(record.name!==pkg.name||record.version!==meta.version||JSON.stringify(record.files.map(f=>f.path).sort())!==JSON.stringify(allowed.sort()))throw Error(`unexpected ${pkg.name} package contents`);
 const data=fs.readFileSync(path.join(output,record.filename));
 entries.push({name:pkg.name,target:pkg.target,filename:record.filename,sha256:crypto.createHash('sha256').update(data).digest('hex'),integrity:'sha512-'+crypto.createHash('sha512').update(data).digest('base64')});
}
fs.writeFileSync(path.join(output,'manifest.json'),JSON.stringify({version:meta.version,packages:entries},null,2)+'\n');
console.log(`Packed and verified ${entries.length} packages for ${meta.version}`);
