import test from "node:test";
import assert from "node:assert/strict";
import { MCPSecurityError, mcpRuleLabel } from "./mcp-security.ts";
test("a result block displays safe diagnostics without retaining raw content", () => {
 const failure = MCPSecurityError.from({stage:"result",reason:"Workspace policy blocks this content.",categories:["pii","jailbreak","pii"],rules:["pii:phone","jailbreak:structural.encoding_density"],matched_content:"private@example.test"});
 assert.equal(failure.stage,"result"); assert.deepEqual(failure.categories,["pii","jailbreak"]);
 assert.equal(mcpRuleLabel(failure.rules[0]),"Phone number pattern");
 assert.equal(mcpRuleLabel(failure.rules[1]),"Long encoded-looking character sequence");
 assert.equal(JSON.stringify(failure).includes("private@example.test"),false);
});
test("legacy and malformed API errors use the normal fallback",()=>{
 assert.equal(MCPSecurityError.from({error:"legacy"}),null);
 assert.equal(MCPSecurityError.from({stage:"unknown",reason:"failure"}),null);
 const failure=MCPSecurityError.from({stage:"arguments",reason:"blocked",categories:[null,"not a safe identifier","pii"],rules:["private@example.test"]});
 assert.deepEqual(failure.categories,["pii"]);assert.deepEqual(failure.rules,[]);
});
