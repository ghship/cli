'use strict';
const fs=require('node:fs');
const {validReleaseTag,validateTrigger,chooseVerification,chooseArtifact,completePages,ghJSON,ghPages,resolveTagCommit,requireMainAncestry}=require('./release_guard.cjs');
const repo=process.env.GITHUB_REPOSITORY, sha=process.env.GITHUB_SHA, ref=process.env.GITHUB_REF, kind=process.env.GITHUB_EVENT_NAME;
const event=JSON.parse(fs.readFileSync(process.env.GITHUB_EVENT_PATH,'utf8'));
const latest=ghJSON(repo,'releases/latest');
if(!latest||!validReleaseTag(latest.tag_name))throw Error('Invalid latest release tag');
const guard=validateTrigger(kind,event,ref,sha,latest,resolveTagCommit(repo,latest.tag_name));
requireMainAncestry(repo,sha);
const runPages=ghPages(repo,`actions/workflows/verification.yml/runs?branch=main&event=push&head_sha=${sha}&per_page=100`);
const runs=completePages(runPages,'workflow_runs');
const run=chooseVerification(runs,sha);
let artifact=`ghship-candidate-${run.run_attempt}`;
if(kind==='workflow_dispatch'){
 const supplied=process.env.RELEASE_RUN;
 if(typeof supplied!=='string'||supplied.trim()!==supplied||!/^[1-9]\d*$/.test(supplied)||Number(supplied)!==run.id)throw Error('Manual recovery must use the newest successful verification run');
 artifact=process.env.RELEASE_ARTIFACT;
}
const artifactPages=ghPages(repo,`actions/runs/${run.id}/artifacts?per_page=100`);
const artifacts=completePages(artifactPages,'artifacts');
chooseArtifact(artifacts,artifact);
fs.writeFileSync(process.env.GHSHIP_GUARD_PATH,JSON.stringify(guard));
fs.appendFileSync(process.env.GITHUB_OUTPUT,`run=${run.id}\nartifact=${artifact}\ntag=${guard.tag}\n`);
console.log(`Selected latest ${guard.tag} and verified main run ${run.id}`);
