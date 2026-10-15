import test from 'node:test';
import assert from 'node:assert/strict';
import { initialTraceTab, isWorkspaceMcpTrace, mcpCallDetails, mcpSecuritySummary, mcpRetentionSummary, mcpPayloadAppliesToSpan } from './mcp-trace-presentation.ts';

const trace = {provider:'mcp', path:'/v1/workspace/mcp/connections/demo/call', status:'ok', tags:{'mcp.tool':'search_products','mcp.source':'manual',remote_action:'completed'}};
const complete = [{name:'mcp.arguments',status:'ok'},{name:'mcp.result',status:'ok'}];

test('successful workspace MCP calls open useful metadata; findings still open Security', () => {
 assert.equal(initialTraceTab(trace,0),'metadata');
 assert.equal(initialTraceTab(trace,1),'security');
 assert.equal(initialTraceTab({provider:'openai'},0),'input');
});
test('do not infer the workspace retention policy for imported MCP traces', () => {
 assert.equal(isWorkspaceMcpTrace({provider:'mcp',path:'/external/tool'}),false);
 assert.equal(isWorkspaceMcpTrace({provider:'openai',path:trace.path}),false);
 assert.equal(isWorkspaceMcpTrace({provider:'mcp',trace_name:'workspace.mcp.search_products'}),true);
 assert.equal(initialTraceTab({provider:'mcp',path:'/external/tool'},0),'input');
});
test('clean status requires both recorded inspections', () => {
 const clean=mcpSecuritySummary(trace,complete,[]);
 assert.equal(clean.tone,'success');
 assert.ok(clean.stages.every(stage=>stage.state==='Completed · no findings recorded'));
 for(const spans of [[],complete.slice(0,1),[{name:'mcp.arguments',status:'error'},complete[1]]]) {
  const result=mcpSecuritySummary(trace,spans,[]);
  assert.notEqual(result.tone,'success');
 }
 assert.equal(mcpSecuritySummary(trace,[],[]).stages[0].state,'Inspection details unavailable');
});
test('argument blocks and unrecorded response checks cannot look clean', () => {
 const blocked=mcpSecuritySummary({...trace,status:'blocked'},[{name:'mcp.arguments',status:'blocked'}],[]);
 assert.equal(blocked.tone,'danger');
 assert.equal(blocked.stages[0].state,'Withheld');
 assert.equal(blocked.stages[1].tone,'neutral');
 assert.equal(blocked.stages[1].state,'Inspection details unavailable');
});
test('failed calls do not inherit a successful security verdict from empty findings', () => {
 const failed=mcpSecuritySummary({...trace,status:'error'},complete.slice(0,1),[]);
 assert.equal(failed.title,'Tool call did not complete');
 assert.equal(failed.tone,'neutral');
});
test('passive and enforced findings remain visible and attributed to the right inspection', () => {
 for(const action_taken of ['pass','warn','mask','block']) {
  const result=mcpSecuritySummary(trace,complete,[{source:'mcp.result',action_taken}]);
  assert.equal(result.title,'Security signals recorded');
  assert.equal(result.stages[0].state,'Completed · no findings recorded');
  assert.equal(result.stages[1].state,'Completed · signals recorded');
 }
});
test('call overview uses only known operational metadata with honest missing values', () => {
 assert.deepEqual(mcpCallDetails(trace),{tool:'search_products',source:'Manual tool test',connection:'Not recorded',remoteAction:'Completed'});
 assert.equal(mcpCallDetails({...trace,tags:{'mcp.source':'assistant',remote_action:'unknown'}}).source,'Assistant conversation');
 assert.equal(mcpCallDetails({...trace,tags:{remote_action:'not_run'}}).remoteAction,'Not executed');
 assert.equal(mcpCallDetails({}).remoteAction,'Not recorded');
 const details=mcpCallDetails({...trace,tags:{...trace.tags,arguments:'sensitive payload',auth:'sensitive credential'}});
 assert.ok(!JSON.stringify(details).includes('sensitive'));
});

test('loading and failed findings requests cannot become an all-clear verdict', () => {
 for(const state of ['loading','error']) {
  const result=mcpSecuritySummary(trace,complete,[],state);
  assert.notEqual(result.tone,'success');
  assert.ok(result.stages.every(stage=>!stage.state.includes('no findings')));
 }
});
test('historical findings without source do not imply either inspection had no findings', () => {
 const result=mcpSecuritySummary(trace,complete,[{action_taken:'warn'}]);
 assert.ok(result.stages.every(stage=>stage.state==='Completed · signal attribution unavailable'));
});


test('retention descriptions use the call snapshot and expose expiry without implying raw capture', () => {
 const before={...trace,tags:{...trace.tags,'mcp.history.mode':'sanitized','mcp.history.state':'retained','mcp.history.expires_at':'2026-10-20T12:00:00Z'}};
 assert.equal(mcpRetentionSummary(before,Date.parse('2026-10-19T12:00:00Z')).label,'Sanitized history');
 assert.equal(mcpRetentionSummary(before,Date.parse('2026-10-21T12:00:00Z')).label,'Expired');
 assert.equal(mcpRetentionSummary(trace).label,'Metadata only');
 assert.equal(mcpRetentionSummary({...trace,tags:{'mcp.history.mode':'raw'}}).mode,'metadata_only');
 const temporary={...trace,tags:{'mcp.history.reason':'temporary_chat'}};
 assert.match(mcpRetentionSummary(temporary).description,/Temporary chat/);
});
test('payload history is scoped to the matching call and inspection direction', () => {
 assert.equal(mcpPayloadAppliesToSpan({id:'t',trace_id:'t',type:'span'},'input'),true);
 assert.equal(mcpPayloadAppliesToSpan({id:'s',trace_id:'t',type:'tool'},'output'),true);
 assert.equal(mcpPayloadAppliesToSpan({id:'s',trace_id:'t',type:'guardrail',name:'mcp.arguments'},'input'),true);
 assert.equal(mcpPayloadAppliesToSpan({id:'s',trace_id:'t',type:'guardrail',name:'mcp.arguments'},'output'),false);
 assert.equal(mcpPayloadAppliesToSpan({id:'s',trace_id:'t',type:'span',name:'mcp.discovery'},'input'),false);
});
