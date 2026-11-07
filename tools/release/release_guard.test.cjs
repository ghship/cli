const {test}=require('node:test');
const assert=require('node:assert/strict');
const {validReleaseTag,validateLatest,chooseVerification,validateTrigger,chooseArtifact,requireAttemptJobs,completePages}=require('./release_guard.cjs');
const sha='a'.repeat(40), other='b'.repeat(40);
const manifest={version:'1.0.8',packages:[{name:'@ghship/cli',filename:'ghship-cli-1.0.8.tgz',sha256:'c'.repeat(64)},{name:'@ghship/cli-linux-x64',filename:'ghship-cli-linux-x64-1.0.8.tgz',sha256:'d'.repeat(64)}]};
const release={id:42,tag_name:'v1.0.8',draft:false,prerelease:false,assets:manifest.packages.map(p=>({name:p.filename,state:'uploaded',digest:'sha256:'+p.sha256}))};
const expected={id:42,tag:'v1.0.8',sha};
test('requires a safe canonical stable version',()=>{
 assert.equal(validReleaseTag('v1.0.8'),true);
 for(const tag of ['v01.0.8','v1.0.8-rc.1','1.0.8','v9007199254740992.0.0','v1.0.8\n'])assert.equal(validReleaseTag(tag),false);
});
test('latest release must match event, version and exact candidate package digests',()=>{
 assert.doesNotThrow(()=>validateLatest(release,expected,manifest));
 for(const bad of [{...release,id:41},{...release,prerelease:true},{...release,draft:true},{...release,tag_name:'v1.0.7'},{...release,assets:[]},{...release,assets:release.assets.map((a,i)=>i?{...a,digest:'sha256:'+ 'f'.repeat(64)}:a)},{...release,assets:[...release.assets,release.assets[0]]}]) assert.throws(()=>validateLatest(bad,expected,manifest));
 assert.throws(()=>validateLatest(release,{...expected,tag:'v1.0.7'},manifest));
});
test('only a current formal latest release event or explicit recovery is accepted',()=>{
 const event={action:'published',release:{id:42,tag_name:'v1.0.8',draft:false,prerelease:false}};
 assert.deepEqual(validateTrigger('release',event,'refs/tags/v1.0.8',sha,release,sha),expected);
 assert.deepEqual(validateTrigger('workflow_dispatch',{},'refs/heads/main',sha,release,sha),expected);
 assert.deepEqual(validateTrigger('workflow_dispatch',{},'refs/tags/v1.0.8',sha,release,sha),expected);
 for(const [kind,ev,ref,current,tagSha] of [
  ['release',{...event,action:'created'},'refs/tags/v1.0.8',release,sha],
  ['release',{...event,release:{...event.release,prerelease:true}},'refs/tags/v1.0.8',release,sha],
  ['release',{...event,release:{...event.release,id:41}},'refs/tags/v1.0.8',release,sha],
  ['release',event,'refs/tags/v1.0.7',release,sha],
  ['release',event,'refs/tags/v1.0.8',{...release,id:43},sha],
  ['release',event,'refs/tags/v1.0.8',release,other],
  ['workflow_dispatch',{},'refs/heads/feature',release,sha],
 ])assert.throws(()=>validateTrigger(kind,ev,ref,sha,current,tagSha));
});
test('requires one unexpired candidate artifact',()=>{
 assert.equal(chooseArtifact([{name:'ghship-candidate-2',expired:false}],'ghship-candidate-2'),'ghship-candidate-2');
 for(const [items,name] of [[[], 'ghship-candidate-2'],[[{name:'ghship-candidate-2',expired:true}],'ghship-candidate-2'],[[{name:'ghship-candidate-2',expired:false},{name:'ghship-candidate-2',expired:false}],'ghship-candidate-2'],[[], '../unsafe']])assert.throws(()=>chooseArtifact(items,name));
});
test('rejects incomplete, changing or possibly capped pagination',()=>{
 assert.deepEqual(completePages([{total_count:2,items:[1]},{total_count:2,items:[2]}],'items'),[1,2]);
 for(const pages of [[],[{items:[]}],[{total_count:2,items:[1]}],[{total_count:2,items:[1]},{total_count:3,items:[2]}],[{total_count:1000,items:[]}]])assert.throws(()=>completePages(pages,'items'));
});
test('selects only the newest successful main push verification for the exact commit',()=>{
 const good={id:20,head_sha:sha,head_branch:'main',event:'push',path:'.github/workflows/verification.yml',status:'completed',conclusion:'success',run_attempt:2};
 assert.equal(chooseVerification([good],sha).id,20);
 assert.throws(()=>chooseVerification([{...good,head_sha:other}],sha));
 for(const bad of [{...good,head_branch:'feature'},{...good,event:'pull_request'},{...good,path:'.github/workflows/other.yml'},{...good,status:'in_progress',conclusion:null},{...good,conclusion:'failure'}])assert.throws(()=>chooseVerification([bad],sha));
 assert.throws(()=>chooseVerification([good,{...good,id:21,conclusion:'failure'}],sha));
});

test('requires packaging and all six native checks in the exact successful attempt',()=>{
 const step=name=>({name,status:'completed',conclusion:'success'});
 const run={id:20,run_attempt:2};
 const names=['npm package',...['linux-amd64','linux-arm64','darwin-amd64','darwin-arm64','windows-amd64','windows-arm64'].map(p=>`native (${p})`)];
 const jobs=names.map(name=>({name,run_id:20,run_attempt:2,status:'completed',conclusion:'success',steps:name==='npm package'?[step('Pack candidate once'),step('Run actions/upload-artifact@abc')]:['Download shared candidate','Verify and extract the exact shared candidate','Test the exact shared candidate'].map(step)}));
 assert.doesNotThrow(()=>requireAttemptJobs(jobs,run));
 const modified=(i,change)=>jobs.map((j,n)=>n===i?{...j,...change}:j);
 for(const bad of [jobs.slice(0,-1),[...jobs,jobs[1]],modified(0,{run_attempt:1}),modified(1,{run_id:19}),modified(2,{conclusion:'skipped'}),modified(3,{steps:[]}),modified(0,{steps:[step('Pack candidate once')]})])assert.throws(()=>requireAttemptJobs(bad,run));
});
