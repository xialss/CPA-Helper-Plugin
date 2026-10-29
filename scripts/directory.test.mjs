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

test('model catalog joins authoritative host fingerprints and persists only routing attributes', () => {
  const credentials = directory({ 'api-keys': { 'openai-compatibility': [
    { name: 'catalog', 'base-url': 'https://catalog.invalid', keys: [{ 'api-key': 'private-upstream-secret', auth_index: 'config-index' }] },
  ] } }, [{ id: 'oauth-fixture.json', auth_index: 'file-index', provider: 'codex', source: 'file', weight: 0 }]);
  const registered = { credentials: credentials.map(c => ({ ref: c.ref, auth_index: c.auth_index, provider: c.provider, models: ['team/public-alias', 'shared'] })) };
  const result = context.cpaModelCatalog(credentials, { contract_version: 'v1', plugin_version: '0.1.12', cpa_version: 'v8.0.45', catalog_revision: 1 }, registered);
  const stored = JSON.parse(JSON.stringify(result));
  assert.equal(stored.credentials.find(c => c.source === 'auth-files').enabled, false);
  assert.deepEqual(stored.credentials[0].models, ['shared', 'team/public-alias']);
  assert.doesNotMatch(JSON.stringify(stored), /private-upstream-secret|auth_id|auth_index|oauth-fixture.json|owned_by|label/);
});

test('host-only model-free credentials do not block a complete ownership catalog', () => {
  const credentials = directory({}, [{ id: 'known.json', auth_index: 'known', provider: 'codex', source: 'file' }]);
  const known = { ref: credentials[0].ref, auth_index: 'known', provider: 'codex', models: [] };
  const omitted = { ref: ref('disabled-runtime-only'), auth_index: 'runtime-index', provider: 'aistudio', models: [] };
  const result = JSON.parse(JSON.stringify(context.cpaModelCatalog(credentials, {}, { credentials: [known, omitted] })));
  assert.deepEqual(result.credentials, [{ ref: known.ref, source: 'auth-files', provider: 'codex', enabled: true, models: [] }]);
  assert.deepEqual(JSON.parse(JSON.stringify(context.cpaModelCatalog([], {}, { credentials: [omitted] }))), { credentials: [] });
});

test('model-free exclusions never hide malformed, duplicate or mismatched identities', () => {
  const omitted = { ref: ref('runtime-only'), auth_index: 'runtime-index', provider: 'aistudio', models: [] };
  for (const change of [{ ref: 'invalid' }, { auth_index: '' }, { auth_index: null }, { auth_index: 'bad\nindex' }, { provider: '' }, { provider: null }, { provider: 'Aistudio' }, { models: null }, { models: ['model'] }]) {
    assert.throws(() => context.cpaModelCatalog([], {}, { credentials: [{ ...omitted, ...change }] }), /CPA/);
  }
  assert.throws(() => context.cpaModelCatalog([], {}, { credentials: [omitted, omitted] }), /CPA/);
  const credentials = directory({}, [{ id: 'known.json', auth_index: 'known', provider: 'codex', source: 'file' }]);
  const known = { ref: credentials[0].ref, auth_index: 'known', provider: 'codex', models: [] };
  for (const change of [{ auth_index: 'other' }, { provider: 'claude' }]) {
    assert.throws(() => context.cpaModelCatalog(credentials, {}, { credentials: [{ ...known, ...change }, omitted] }), /CPA/);
  }
  assert.throws(() => context.cpaModelCatalog(credentials, {}, { credentials: [omitted] }), /CPA/);
});

test('normalized Host identity retains all excluded model-free entries for double reads', () => {
  const known = { ref: ref('known'), auth_index: 'known', provider: 'codex', models: ['shared', 'team/alias'] };
  const omitted = { ref: ref('runtime-only'), auth_index: 'runtime-index', provider: 'aistudio', models: [] };
  const identity = registered => JSON.stringify(context.cpaRegisteredModels({ credentials: registered }));
  const before = identity([known, omitted]);
  assert.equal(identity([omitted, { ...known, models: ['team/alias', 'shared'] }]), before);
  for (const change of [{ ref: ref('different-runtime-only') }, { auth_index: 'other' }, { provider: 'codex' }, { models: ['new-owner'] }]) {
    assert.notEqual(identity([known, { ...omitted, ...change }]), before);
  }
  assert.notEqual(identity([known]), before);
  assert.notEqual(identity([known, omitted, { ...omitted, ref: ref('another'), auth_index: 'another' }]), before);
});

test('missing, malformed and unconfirmed host models reject the complete synchronization', () => {
  const credentials = directory({}, [{ id: 'oauth.json', auth_index: 'idx', provider: 'claude', source: 'file' }]);
  const entry = { ref: credentials[0].ref, auth_index: 'idx', provider: 'claude', models: ['model'] };
  for (const result of [{}, { credentials: null }, { credentials: [] }, { credentials: [{}] }, { credentials: [{ ...entry, models: null }] }, { credentials: [{ ...entry, models: ['bad\nmodel'] }] }, { credentials: [entry, entry] }, { credentials: [{ ...entry, ref: ref('different-runtime-id') }] }, { credentials: [{ ...entry, auth_index: 'different-index' }] }, { credentials: [{ ...entry, provider: 'codex' }] }]) {
    assert.throws(() => context.cpaModelCatalog(credentials, {}, result), /CPA/);
  }
});

test('shared indexes retain distinct host identities and model-set changes remain observable', () => {
  const credentials = directory({}, [
    { id: 'a', auth_index: 'shared', provider: 'codex', source: 'memory', status: 'active' },
    { id: 'b', auth_index: 'shared', provider: 'codex', source: 'memory', disabled: true },
  ]);
  const registered = { credentials: credentials.map(c => ({ ref: c.ref, auth_index: c.auth_index, provider: c.provider, models: ['team/alias', 'shared'] })) };
  const first = JSON.stringify(context.cpaModelCatalog(credentials, {}, registered));
  assert.equal(context.cpaModelCatalog(credentials, {}, registered).credentials.length, 2);
  registered.credentials.reverse();
  assert.equal(JSON.stringify(context.cpaModelCatalog(credentials, {}, registered)), first);
  registered.credentials[0].models.push('new-model');
  assert.notEqual(JSON.stringify(context.cpaModelCatalog(credentials, {}, registered)), first);
});

test('directory identity ignores ordering and transient errors but detects routing changes', () => {
  const credentials = directory({}, [
    { id: 'a.json', auth_index: 'a', provider: 'codex', source: 'file', status: 'active' },
    { id: 'b.json', auth_index: 'b', provider: 'claude', source: 'file', status: 'error' },
  ]);
  const identity = context.cpaDirectoryIdentity(credentials);
  const changed = structuredClone(credentials).reverse();
  changed[0].status = 'active';
  assert.equal(context.cpaDirectoryIdentity(changed), identity);
  changed[0].status = 'disabled';
  assert.notEqual(context.cpaDirectoryIdentity(changed), identity);
});
