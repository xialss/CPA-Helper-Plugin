import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import { createHash } from 'node:crypto';
import assert from 'node:assert/strict';
import test from 'node:test';

const sha256 = value => createHash('sha256').update(value).digest('hex');
const context = vm.createContext({ sha256 });
vm.runInContext(await readFile(new URL('../internal/plugin/web/directory.js', import.meta.url), 'utf8'), context);
const directory = (config, files) => JSON.parse(JSON.stringify(context.cpaCredentialDirectory(config, { files })));
const ref = id => 'sha256:' + sha256('cpa-key-billing:credential:v1\0' + id);

test('v8 configured credentials remain selectable when absent from the file directory', () => {
  const config = { 'api-keys': { 'openai-compatibility': [
    { name: 'first', 'base-url': 'https://first.invalid', keys: [{ 'api-key': 'shared-secret-token', auth_index: 'index-a' }] },
    { name: 'second', 'base-url': 'https://second.invalid', keys: [{ 'api-key': 'shared-secret-token', auth_index: 'index-b' }] },
    { name: 'keyless', 'base-url': 'https://keyless.invalid', keys: [], auth_index: 'index-c' },
    { name: 'disabled', disabled: true, keys: [{ 'api-key': 'not-in-runtime' }] },
  ] } };
  const result = directory(config, []);
  assert.equal(result.length, 3);
  assert.deepEqual(result.map(c => c.auth_index), ['index-a', 'index-b', 'index-c']);
  assert.match(result[0].label, /^first/);
  assert.match(result[1].label, /^second/);
  assert.match(result[2].label, /^keyless/);
  assert.equal(result[0].ref, ref('openai-compatibility:first:' + sha256('openai-compatibility:first\0shared-secret-token\0https://first.invalid\0').slice(0, 12)));
  assert.doesNotMatch(JSON.stringify(result), /shared-secret-token|not-in-runtime/);
  config['api-keys']['openai-compatibility'].reverse();
  assert.deepEqual(directory(config, []).reverse(), result);
});

test('file credentials and host-only runtime entries retain actual identities and status', () => {
  const result = directory({}, [
    { id: 'oauth.json', auth_index: 'oauth', provider: 'claude', source: 'file', name: 'OAuth account', disabled: true },
    { id: 'runtime', auth_index: 'runtime', type: 'codex', source: 'memory', status: 'error' },
  ]);
  assert.equal(result[0].ref, ref('oauth.json'));
  assert.equal(result[0].source, 'auth-files');
  assert.equal(result[0].status, 'disabled');
  assert.equal(result[1].source, 'ai-providers');
  assert.equal(result[1].status, 'error');
});

test('malformed and ambiguous runtime identities fail explicitly', () => {
  const entry = { id: 'opaque', auth_index: 'idx' };
  for (const files of [[{ id: 'missing-index' }], [entry, entry]]) {
    assert.throws(() => directory({}, files), /CPA/);
  }
  assert.throws(() => directory({ 'api-keys': [] }, []), /CPA/);
  assert.throws(() => directory({ 'api-keys': { codex: [{}] } }, []), /CPA/);
  assert.equal(directory({}, [entry, { id: 'other', auth_index: 'idx' }]).length, 2);
});

test('group inheritance, normalized headers, duplicate IDs and shared indexes preserve scheduler identity', () => {
  const group = { 'base-url': 'https://codex.invalid', 'proxy-url': 'http://proxy.invalid', prefix: '/team/', headers: { ' X-Test ': ' value ' }, keys: [
    { 'api-key': 'key', auth_index: 'shared' },
    { 'api-key': 'key', auth_index: 'shared', disabled: true },
    { 'api-key': 'key', auth_index: 'shared', 'proxy-url': '', headers: {} },
  ] };
  const result = directory({ 'api-keys': { codex: [group] } }, []);
  const id = 'codex:apikey:' + sha256('codex:apikey\0key\0https://codex.invalid\0http://proxy.invalid\0team\0X-Test\0value\0').slice(0, 12);
  assert.equal(result[0].ref, ref(id));
  assert.equal(result[1].ref, ref(id + '-1'));
  assert.equal(result[1].status, 'disabled');
  assert.notEqual(result[2].ref, result[0].ref);
  assert.equal(directory({ 'api-keys': { gemini: [group] } }, []).length, 2);
});

test('prototype-named headers participate in the CPA scheduler ID', () => {
  for (const provider of ['claude', 'codex']) {
    for (const location of ['group', 'key']) {
      // JSON decoding must preserve __proto__ as an own property, like CPA's API.
      const headers = JSON.parse('{"__proto__":"fixture-header","constructor":"fixture-constructor"}');
      const key = { 'api-key': 'fixture-key', auth_index: 'fixture-index' };
      const group = { 'base-url': 'https://fixture.invalid', keys: [key] };
      (location === 'group' ? group : key).headers = headers;
      const [credential] = directory({ 'api-keys': { [provider]: [group] } }, []);
      const kind = provider + ':apikey';
      const id = kind + ':' + sha256(kind + '\0fixture-key\0https://fixture.invalid\0\0\0__proto__\0fixture-header\0constructor\0fixture-constructor\0').slice(0, 12);
      assert.equal(credential.ref, ref(id), `${provider} ${location} headers`);
    }
  }
});

test('keyless null lists follow the v8 management contract without accepting malformed lists', () => {
  const group = { name: 'keyless-null', 'base-url': 'https://keyless.invalid', keys: null, auth_index: 'keyless-index' };
  const config = { 'api-keys': { 'openai-compatibility': [group] } };
  const result = directory(config, []);
  assert.equal(result.length, 1);
  assert.equal(result[0].auth_index, 'keyless-index');
  group.keys = [];
  assert.deepEqual(directory(config, []), result);
  for (const keys of ['', false, 0, {}, undefined]) {
    group.keys = keys;
    assert.throws(() => directory(config, []), /CPA/);
  }
  assert.throws(() => directory({ 'api-keys': { codex: [{ keys: null }] } }, []), /CPA/);
});
