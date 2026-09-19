import assert from 'node:assert/strict';
import { createHash, randomUUID } from 'node:crypto';

const origin = process.env.CPA_URL;
const key = process.env.CPA_MANAGEMENT_KEY;
if (!origin || !key) throw new Error('CPA_URL and CPA_MANAGEMENT_KEY are required');
const base = '/v0/management/plugins/cpa-helper-plugin/v1';
async function call(path, options = {}) {
  const response = await fetch(new URL(path, origin), {
    ...options,
    headers: { Authorization: `Bearer ${key}`, ...options.headers },
    signal: AbortSignal.timeout(15000),
  });
  const body = await response.json();
  assert.equal(response.status, options.expected ?? 200, `Unexpected response: ${response.status}`);
  return body;
}
const listing = await call('/v0/management/plugins');
const plugin = listing.plugins.find(p => p.id === 'cpa-helper-plugin');
assert.equal(plugin?.effective_enabled, true);
assert(plugin.menus.some(m => m.path === '/v0/resource/plugins/cpa-helper-plugin/ui'));
const original = await call(base + '/policy');
// Run against a newly installed unrestricted policy only; never alter user rules.
assert.equal(original.keys.length, 0, 'Live smoke requires an empty key policy');
assert.equal(original.groups.length, 0, 'Live smoke requires an empty group policy');
const directory = await call('/v0/management/api-keys');
const existingKey = directory['api-keys'][0];
assert.equal(typeof existingKey, 'string');
const models = await call('/v1/models', { headers: { Authorization: `Bearer ${existingKey}` } });
const sentinel = models.data[0]?.id;
assert.equal(typeof sentinel, 'string', 'A real registered model is required');
const downstream = `cpa-helper-acceptance-${randomUUID()}`;
const draft = structuredClone(original);
draft.policy_revision++;
draft.generated_at = new Date().toISOString();
draft.keys = [{
  id: createHash('sha256').update('cli-proxy-api:caller-scope:v1\0' + downstream.trim()).digest('hex'),
  enabled: true, group_ids: [], max_concurrency: 0,
  rule: { denied_models: [sentinel] },
}];
const transaction = randomUUID();
// Cleanup also runs when a write response is lost after the server accepted it.
try {
  await call(base + '/policy', {
    method: 'PUT', body: JSON.stringify(draft),
    headers: { 'Content-Type': 'application/json', 'If-Match': String(original.policy_revision), 'Idempotency-Key': transaction },
  });
  await call('/v0/management/api-keys', {
    method: 'PATCH', body: JSON.stringify({ old: downstream, new: downstream }),
    headers: { 'Content-Type': 'application/json' },
  });
  const readyBy = Date.now() + 10000;
  while (true) {
    const probe = await fetch(new URL('/v1/models', origin), {
      headers: { Authorization: `Bearer ${downstream}` }, signal: AbortSignal.timeout(2000),
    });
    await probe.arrayBuffer();
    if (probe.ok) break;
    assert.equal(probe.status, 401, 'Unexpected readiness response');
    assert(Date.now() < readyBy, 'CPA did not activate the temporary key');
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  const denied = await call('/v1/chat/completions', {
    method: 'POST', expected: 403,
    headers: { Authorization: `Bearer ${downstream}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ model: sentinel, messages: [{ role: 'user', content: 'acceptance' }] }),
  });
  assert.equal(denied.error.code, 'model_forbidden');
  console.log('Live CPA: official menu registered, policy published, request denied with 403/model_forbidden.');
} finally {
  await call('/v0/management/api-keys?value=' + encodeURIComponent(downstream), { method: 'DELETE' });
  const remaining = await call('/v0/management/api-keys');
  assert(!remaining['api-keys'].includes(downstream), 'Temporary key cleanup failed');
  const current = await call(base + '/policy');
  if (JSON.stringify(current) === JSON.stringify(original)) {
    console.log('Policy write was not applied; original policy remains active.');
  } else {
    assert.equal(current.policy_revision, draft.policy_revision, 'Policy changed concurrently; automatic rollback stopped');
    assert.equal(current.keys.length, 1, 'Policy changed concurrently; automatic rollback stopped');
    assert.deepEqual(current.keys[0].rule.denied_models, [sentinel]);
    await call(base + '/policy/rollback', {
      method: 'POST', body: JSON.stringify({ policy_revision: original.policy_revision }),
      headers: { 'Content-Type': 'application/json', 'If-Match': String(draft.policy_revision), 'Idempotency-Key': transaction + '-rollback' },
    });
    const restored = await call(base + '/policy');
    assert.deepEqual(restored.keys, original.keys);
    assert.deepEqual(restored.groups, original.groups);
    console.log(`Original policy restored at revision ${restored.policy_revision}.`);
  }
}
const health = await call(base + '/health');
assert.equal(health.status, 'ok');
console.log('Live CPA health: ok. No real upstream generation was requested.');
