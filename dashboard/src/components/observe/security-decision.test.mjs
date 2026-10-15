import test from 'node:test';
import assert from 'node:assert/strict';
import { securityDecision, hasActionableFindings, onlyPassiveFindings } from './security-decision.ts';

test('pass and recorded-only signals do not imply an attack or enforcement', () => {
 for (const action of ['pass', 'log', 'log_only', undefined, 'future_action']) {
  assert.equal(securityDecision(action).tone, 'neutral');
  assert.equal(hasActionableFindings([{action_taken: action}]), false);
 }
});
test('effective actions preserve blocking, warning and protection states', () => {
 assert.equal(securityDecision('block').tone, 'danger');
 assert.equal(securityDecision('warn').tone, 'warning');
 assert.equal(securityDecision('mask').tone, 'success');
 assert.equal(securityDecision('tokenize').tone, 'success');
 assert.equal(hasActionableFindings([{action_taken:'pass'}, {action_taken:'block'}]), true);
});

test('missing historical actions are not inferred to be pass decisions', () => {
 assert.equal(onlyPassiveFindings([{action_taken:'pass'}]), true);
 assert.equal(onlyPassiveFindings([{action_taken:'pass'}, {}]), false);
 assert.equal(onlyPassiveFindings([]), false);
});
