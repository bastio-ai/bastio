import assert from "node:assert/strict";
import test from "node:test";
import {workspaceModelChoices} from "./workspace-models.ts";
test("workspace discovery never invents catalogue access or exposes disconnected providers",()=>{
 const choices=workspaceModelChoices({restricted:false,providers:[{provider:"openai",configured:true,source:"provider",models:[{id:"gpt-4.1-nano"}]},{provider:"anthropic",configured:false,source:"catalog",models:[]}]});
 assert.deepEqual(choices.map((item)=>item.model),["gpt-4.1-nano"]);assert.equal(choices[0].badge,"Not tested");
});
test("restricted catalogue providers cannot repopulate models outside an employee allowlist",()=>{
 const choices=workspaceModelChoices({restricted:true,providers:[{provider:"gemini",configured:true,source:"catalog",models:[{id:"gemini-team-deployment"}]}]});
 assert.deepEqual(choices.map((item)=>item.model),["gemini-team-deployment"]);
});
test("dated tests sort first and confirmed provider access failures are withheld",()=>{
 const choices=workspaceModelChoices({restricted:false,providers:[{provider:"openai",configured:true,source:"provider",models:[{id:"gpt-6-luna",check_status:"failed",error_code:"model_access_denied"},{id:"gpt-4o-mini",check_status:"passed",checked_at:"2026-10-14T08:00:00Z"},{id:"gpt-4.1-nano",check_status:"failed",error_code:"workspace_security_policy"}]}]});
 assert.deepEqual(choices.map((item)=>item.model),["gpt-4o-mini","gpt-4.1-nano"]);assert.equal(choices[0].badge,"Test passed");assert.match(choices[0].description,/Last successful test/);
});
