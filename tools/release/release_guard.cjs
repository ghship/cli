'use strict';
const fs=require('node:fs');
const {spawnSync}=require('node:child_process');
function validReleaseTag(tag){
 if(typeof tag!=='string'||tag.trim()!==tag||!/^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(tag))return false;
 return tag.slice(1).split('.').every(part=>Number.isSafeInteger(Number(part)));
}
function validateLatest(release,expected,manifest){
 if(!expected||!Number.isSafeInteger(expected.id)||expected.id<1||!validReleaseTag(expected.tag)||typeof expected.sha!=='string'||expected.sha.length!==40||!/^[a-f0-9]{40}$/.test(expected.sha))throw Error('Invalid expected release');
 if(!release||release.id!==expected.id||release.tag_name!==expected.tag||release.draft!==false||release.prerelease!==false)throw Error('Release is not the current formal latest');
 if(!manifest||`v${manifest.version}`!==expected.tag||!Array.isArray(manifest.packages)||!Array.isArray(release.assets))throw Error('Release version or assets do not match candidate');
 for(const p of manifest.packages){
  if(typeof p.filename!=='string'||p.filename.trim()!==p.filename||!/^ghship-cli-[a-z0-9.-]+\.tgz$/.test(p.filename)||typeof p.sha256!=='string'||p.sha256.length!==64||!/^[a-f0-9]{64}$/.test(p.sha256))throw Error('Invalid candidate package');
  const matches=release.assets.filter(a=>a.name===p.filename);
  if(matches.length!==1||matches[0].state!=='uploaded'||matches[0].digest!==`sha256:${p.sha256}`)throw Error(`Release asset does not match candidate: ${p.name}`);
 }
}
function chooseVerification(runs,sha){
 if(typeof sha!=='string'||sha.length!==40||!/^[a-f0-9]{40}$/.test(sha)||!Array.isArray(runs))throw Error('Invalid verification query');
 const matching=runs.filter(r=>r.head_sha===sha).sort((a,b)=>b.id-a.id);
 const run=matching[0];
 if(!run||!Number.isSafeInteger(run.id)||run.head_branch!=='main'||run.event!=='push'||run.path!=='.github/workflows/verification.yml'||run.status!=='completed'||run.conclusion!=='success'||!Number.isSafeInteger(run.run_attempt)||run.run_attempt<1)throw Error('No current successful main verification for release commit');
 return run;
}
function validateTrigger(eventName,event,ref,sha,latest,tagCommit){
 if(!latest||!Number.isSafeInteger(latest.id)||latest.id<1||latest.draft!==false||latest.prerelease!==false||!validReleaseTag(latest.tag_name)||typeof sha!=='string'||sha.length!==40||!/^[a-f0-9]{40}$/.test(sha)||tagCommit!==sha)throw Error('Latest release or tag does not match workflow commit');
 if(eventName==='release'){
  if(event?.action!=='published'||event.release?.id!==latest.id||event.release?.tag_name!==latest.tag_name||event.release?.draft!==false||event.release?.prerelease!==false||ref!==`refs/tags/${latest.tag_name}`)throw Error('Release event is not the current formal latest');
 }else if(eventName==='workflow_dispatch'){
  if(ref!=='refs/heads/main'&&ref!==`refs/tags/${latest.tag_name}`)throw Error('Manual recovery must target main or the latest release tag');
 }else throw Error('Unsupported release trigger');
 return {id:latest.id,tag:latest.tag_name,sha};
}
function requireAttemptJobs(jobs,run){
 if(!Array.isArray(jobs)||!Number.isSafeInteger(run?.id)||!Number.isSafeInteger(run?.run_attempt))throw Error('Invalid verification jobs');
 const names=['npm package',...['linux-amd64','linux-arm64','darwin-amd64','darwin-arm64','windows-amd64','windows-arm64'].map(p=>`native (${p})`)];
 for(const name of names){
  const matches=jobs.filter(j=>j.name===name);
  if(matches.length!==1)throw Error(`Missing or ambiguous verification job: ${name}`);
  const job=matches[0];
  if(job.run_id!==run.id||job.run_attempt!==run.run_attempt||job.status!=='completed'||job.conclusion!=='success'||!Array.isArray(job.steps))throw Error(`Verification job did not pass in the candidate attempt: ${name}`);
  const required=name==='npm package'?['Pack candidate once']:['Download shared candidate','Verify and extract the exact shared candidate','Test the exact shared candidate'];
  for(const step of required){
   const matches=job.steps.filter(s=>s.name===step);
   if(matches.length!==1||matches[0].status!=='completed'||matches[0].conclusion!=='success')throw Error(`Required verification step did not pass: ${name}: ${step}`);
  }
  if(name==='npm package'){
   const upload=job.steps.filter(s=>typeof s.name==='string'&&s.name.startsWith('Run actions/upload-artifact@'));
   if(upload.length!==1||upload[0].status!=='completed'||upload[0].conclusion!=='success')throw Error('Candidate upload did not pass');
  }
 }
}
function chooseArtifact(artifacts,name){
 if(typeof name!=='string'||name.trim()!==name||!/^ghship-candidate-[1-9]\d*$/.test(name)||!Array.isArray(artifacts))throw Error('Invalid candidate artifact');
 const matches=artifacts.filter(a=>a.name===name&&a.expired===false);
 if(matches.length!==1)throw Error('Verified candidate artifact is missing or ambiguous');
 return name;
}
function completePages(pages,key){
 if(!Array.isArray(pages)||pages.length===0)throw Error('Incomplete GitHub results');
 const total=pages[0]?.total_count;
 if(!Number.isSafeInteger(total)||total<0||total>=1000)throw Error('GitHub result count is unknown or may be truncated');
 const all=pages.flatMap(page=>{if(page.total_count!==total||!Array.isArray(page[key]))throw Error('GitHub results changed during pagination');return page[key];});
 if(all.length!==total)throw Error('Incomplete GitHub results');
 return all;
}
function ghJSON(repo,endpoint){
 if(typeof repo!=='string'||repo.trim()!==repo||!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repo))throw Error('Invalid repository');
 const r=spawnSync('gh',['api',`repos/${repo}/${endpoint}`],{encoding:'utf8'});
 if(r.error||r.status!==0)throw Error(`Cannot verify GitHub release: ${r.stderr?.trim()||r.error?.message||'unknown error'}`);
 return JSON.parse(r.stdout);
}
function ghPages(repo,endpoint){
 if(typeof repo!=='string'||repo.trim()!==repo||!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repo))throw Error('Invalid repository');
 const r=spawnSync('gh',['api','--paginate','--slurp',`repos/${repo}/${endpoint}`],{encoding:'utf8'});
 if(r.error||r.status!==0)throw Error(`Cannot query GitHub verification: ${r.stderr?.trim()||r.error?.message||'unknown error'}`);
 const pages=JSON.parse(r.stdout);if(!Array.isArray(pages))throw Error('Invalid GitHub pagination');return pages;
}
function resolveTagCommit(repo,tag){
 if(!validReleaseTag(tag))throw Error('Invalid release tag');
 let object=ghJSON(repo,`git/ref/tags/${tag}`).object;
 for(let i=0;object?.type==='tag'&&i<5;i++){
  if(typeof object.sha!=='string'||object.sha.length!==40||!/^[a-f0-9]{40}$/.test(object.sha))throw Error('Invalid annotated tag');
  object=ghJSON(repo,`git/tags/${object.sha}`).object;
 }
 if(!object||object.type!=='commit'||typeof object.sha!=='string'||object.sha.length!==40||!/^[a-f0-9]{40}$/.test(object.sha))throw Error('Release tag does not resolve to a commit');
 return object.sha;
}
function requireMainAncestry(repo,sha){
 if(typeof sha!=='string'||sha.length!==40||!/^[a-f0-9]{40}$/.test(sha))throw Error('Invalid release commit');
 const comparison=ghJSON(repo,`compare/${sha}...main`);
 if(!comparison||!['ahead','identical'].includes(comparison.status)||comparison.base_commit?.sha!==sha)throw Error('Release commit is not on main');
}
function guardLatest(repo,expected,manifest){
 validateLatest(ghJSON(repo,'releases/latest'),expected,manifest);
 if(resolveTagCommit(repo,expected.tag)!==expected.sha)throw Error('Release tag moved after verification');
 requireMainAncestry(repo,expected.sha);
}
function guardFromEnvironment(manifest){
 const file=process.env.GHSHIP_RELEASE_GUARD;
 if(!file)throw Error('GitHub release guard is required');
 const guard=JSON.parse(fs.readFileSync(file,'utf8'));
 guardLatest(process.env.GITHUB_REPOSITORY,guard,manifest);
}
module.exports={validReleaseTag,validateLatest,chooseVerification,validateTrigger,chooseArtifact,requireAttemptJobs,completePages,ghJSON,ghPages,resolveTagCommit,requireMainAncestry,guardLatest,guardFromEnvironment};
