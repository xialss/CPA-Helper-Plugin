import { chromium } from 'playwright';
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, copyFile, writeFile, readFile } from 'node:fs/promises';
import { createWriteStream } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const binary = process.env.CPA_BINARY;
if (!binary) throw new Error('CPA_BINARY must point to CPA v8.0.45');
const serve = process.argv.includes('--serve');
const dir = await mkdtemp(path.join(tmpdir(), 'cpa-helper-ui-'));
await mkdir(path.join(dir, 'plugins'));
await mkdir(path.join(dir, 'auth'));
await writeFile(path.join(dir, 'auth', 'file-fixture.json'), JSON.stringify({ type: 'codex', email: 'file-fixture@example.invalid', access_token: 'synthetic-file-token', prefix: 'file' }), { mode: 0o600 });
await writeFile(path.join(dir, 'auth', 'disabled-fixture.json'), JSON.stringify({ type: 'codex', email: 'disabled-fixture@example.invalid', disabled: true }), { mode: 0o600 });
const ext = process.platform === 'win32' ? 'dll' : 'so';
await copyFile(process.env.CPA_PLUGIN_BINARY || path.join(root, 'dist', `cpa-helper-plugin.${ext}`), path.join(dir, 'plugins', `cpa-helper-plugin.${ext}`));
let claudeRequests = 0;
const upstream = createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'application/json' });
  if (req.url.startsWith('/v1/messages')) {
    claudeRequests++;
    res.end(JSON.stringify({ id: 'header-fixture', type: 'message', role: 'assistant', model: 'header-model', content: [{ type: 'text', text: 'Header fixture' }], stop_reason: 'end_turn', usage: { input_tokens: 1, output_tokens: 1 } }));
  } else res.end(JSON.stringify({ id: 'preview', object: 'chat.completion', model: 'demo-model', choices: [{ index: 0, message: { role: 'assistant', content: 'Local fixture' }, finish_reason: 'stop' }] }));
});
await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve));
const probe = createServer(); await new Promise(resolve => probe.listen(0, '127.0.0.1', resolve));
const port = probe.address().port; await new Promise(resolve => probe.close(resolve));
const posix = value => value.replaceAll('\\', '/');
const config = {
  host: '127.0.0.1', port, 'auth-dir': './auth',
  'ws-auth': false,
  'api-keys': ['preview-key-alpha', 'preview-key-beta'],
  'remote-management': { 'secret-key': 'preview-management', 'allow-remote': false, 'disable-control-panel': true, 'disable-auto-update-panel': true },
  'request-retry': 0,
  plugins: { enabled: true, dir: posix(path.join(dir, 'plugins')), configs: { 'cpa-helper-plugin': { enabled: true, state_dir: posix(path.join(dir, 'state')), response_model_mismatch: { enabled: true, unknown_action: 'reject' } } } },
  'openai-compatibility': [
    { name: 'preview', 'base-url': `http://127.0.0.1:${upstream.address().port}/v1`, 'api-key-entries': [{ 'api-key': 'preview-upstream' }], models: [{ name: 'demo-model', alias: 'demo-model' }, { name: 'restricted-model', alias: 'restricted-model' }] },
    { name: 'keyless-null', prefix: 'team', 'base-url': `http://127.0.0.1:${upstream.address().port}/v1`, 'api-key-entries': null, models: [{ name: 'keyless-model', alias: 'keyless-model' }] },
    { name: 'shared-index', 'base-url': `http://127.0.0.1:${upstream.address().port}/v1`, 'api-key-entries': [{ 'api-key': 'shared-fixture' }, { 'api-key': 'shared-fixture', weight: 0 }], models: [{ name: 'shared-model', alias: 'shared-model' }] },
  ],
  'claude-api-key': [{ 'api-key': 'header-fixture-key', 'base-url': `http://127.0.0.1:${upstream.address().port}`, headers: JSON.parse('{"__proto__":"fixture-header"}'), 'excluded-models': ['removed-model'], models: [{ name: 'header-model', alias: 'header-model' }, { name: 'removed-model', alias: 'removed-model' }] }],
};
await writeFile(path.join(dir, 'config.yaml'), JSON.stringify(config), { mode: 0o600 });
const log = createWriteStream(path.join(dir, 'cpa.log'));
const env = Object.fromEntries(['PATH', 'SystemRoot', 'WINDIR', 'TEMP', 'TMP', 'HOME'].filter(k => process.env[k]).map(k => [k, process.env[k]]));
const child = spawn(binary, ['--config', path.join(dir, 'config.yaml'), '--local-model'], { cwd: dir, env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
child.stdout.pipe(log); child.stderr.pipe(log);
let stopped = false;
let runtimeSocket;
async function cleanup() { if (stopped) return; stopped = true; runtimeSocket?.close(); if (child.exitCode === null) { const exit = new Promise(resolve => child.once('exit', resolve)); child.kill(); await exit; } await new Promise(resolve => upstream.close(resolve)); log.end(); }
process.on('SIGINT', () => cleanup().then(() => process.exit()));
process.on('SIGTERM', () => cleanup().then(() => process.exit()));
const base = `http://127.0.0.1:${port}`;
const ui = base + '/v0/resource/plugins/cpa-helper-plugin/ui';
try {
  let ready = false;
  for (let attempt = 0; attempt < 120; attempt++) {
    if (child.exitCode !== null) throw new Error(`CPA exited: ${await readFile(path.join(dir, 'cpa.log'), 'utf8')}`);
    try { const response = await fetch(base + '/v0/management/plugins/cpa-helper-plugin/v1/health', { headers: { Authorization: 'Bearer preview-management' }, signal: AbortSignal.timeout(1000) }); ready = response.ok; } catch (e) { if (e.name !== 'TimeoutError' && e.cause?.code !== 'ECONNREFUSED') throw e; }
    if (ready) break; await new Promise(resolve => setTimeout(resolve, 250));
  }
  if (!ready) throw new Error(`CPA did not load: ${await readFile(path.join(dir, 'cpa.log'), 'utf8')}`);
  if (serve) { console.log(JSON.stringify({ url: ui, managementKey: 'preview-management', isolatedDirectory: dir, pid: child.pid })); }
  else {
    const browser = await chromium.launch({ channel: 'msedge', headless: true });
    try {
      const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
      const errors = []; page.on('pageerror', error => errors.push(error.message));
      let modelRefreshRequests = 0;
      page.on('request', request => { if (new URL(request.url()).pathname === '/v8/management/models/refresh') modelRefreshRequests++; });
      const hostRequests = [];
      page.on('request', request => {
        if (new URL(request.url()).pathname.startsWith('/v0/management/') && !new URL(request.url()).pathname.startsWith('/v0/management/plugins/cpa-helper-plugin/v1/')) hostRequests.push(request.url());
      });
      await page.addInitScript(() => {
        const value = JSON.stringify({ state: { managementKey: 'preview-management', apiBase: location.origin, rememberPassword: true }, version: 0 });
        const secret = new TextEncoder().encode('cli-proxy-api-webui::secure-storage|' + location.host + '|' + navigator.userAgent);
        const bytes = new TextEncoder().encode(value);
        localStorage.setItem('cli-proxy-auth', 'enc::v1::' + btoa(Array.from(bytes, (b, i) => String.fromCharCode(b ^ secret[i % secret.length])).join('')));
      });
      await page.goto(ui);
      await page.waitForFunction(() => document.querySelector('#key-rows').children.length === 2);
      await page.waitForFunction(() => !document.querySelector('#sync').disabled);
      assert.equal(await page.locator('#message.error').count(), 0, await page.locator('#message').textContent());
      // Capture the legacy null contract before other tests write v8 config.
      const managementHeaders = { Authorization: 'Bearer preview-management' };
      const currentConfig = await (await page.request.get(base + '/v8/management/config', { headers: managementHeaders })).json();
      const keyless = currentConfig['api-keys']['openai-compatibility'].find(group => group.name === 'keyless-null');
      assert.equal(keyless.keys, null);
      assert.equal(typeof keyless.auth_index, 'string');
      // Decode in the page, as production does; passing __proto__ through the
      // automation argument serializer can itself lose that property.
      const inventory = await page.evaluate(async () => {
        const headers = { Authorization: 'Bearer preview-management' };
        const config = await (await fetch('/v8/management/config', { headers })).json();
        const files = await (await fetch('/v8/management/credentials', { headers })).json();
        if (!Object.hasOwn(config['api-keys'].claude[0].headers, '__proto__')) throw new Error('CPA header fixture missing');
        return cpaCredentialDirectory(config, files);
      });
      assert(inventory.some(item => item.auth_index === keyless.auth_index && item.provider === 'openai-compatible-keyless-null'));
      const catalogURL = base + '/v0/management/plugins/cpa-helper-plugin/v1/model-catalog';
      const readCatalog = async () => (await page.request.get(catalogURL, { headers: managementHeaders })).json();
      let synchronized = await readCatalog();
      assert.equal(synchronized.health.status, 'ok');
      assert.equal(synchronized.snapshot.credentials.length, inventory.length);
      const shared = inventory.filter(item => item.provider === 'openai-compatible-shared-index');
      assert.equal(shared.length, 2);
      assert.equal(shared[0].auth_index, shared[1].auth_index, 'Fixture must exercise non-unique auth_index');
      assert.notEqual(shared[0].ref, shared[1].ref);
      assert.deepEqual(shared.map(item => synchronized.snapshot.credentials.find(c => c.ref === item.ref).enabled).sort(), [false, true]);
      for (const name of ['file-fixture.json', 'disabled-fixture.json']) {
        const file = inventory.find(item => item.auth_id === name);
        assert(file, 'Relative auth-dir must preserve the actual file ID: ' + name);
        const saved = synchronized.snapshot.credentials.find(c => c.ref === file.ref);
        assert.equal(saved.source, 'auth-files');
        assert.equal(saved.enabled, name === 'file-fixture.json');
        if (saved.enabled) assert(saved.models.some(model => model.startsWith('file/')), 'File models lost the public prefix');
      }
      assert(synchronized.snapshot.credentials.find(c => c.provider === 'openai-compatible-keyless-null').models.includes('team/keyless-model'), 'registered public prefix missing');
      assert.deepEqual(synchronized.snapshot.credentials.find(c => c.provider === 'claude').models, ['header-model'], 'CPA-excluded models entered the plugin catalog');
      assert.doesNotMatch(JSON.stringify(synchronized.snapshot), /preview-upstream|preview-management|auth_id|auth_index|header-fixture-key/);
      assert.equal(modelRefreshRequests, 0, 'Opening the panel must not refresh external model sources');
      // CPA reads legacy null but its v8 write validator requires a list. Normalize
      // this isolated fixture only, so unrelated configuration-save tests can run.
      keyless.keys = [];
      const normalized = await page.request.put(base + '/v8/management/config/api-keys/openai-compatibility', {
        headers: managementHeaders, data: currentConfig['api-keys']['openai-compatibility'],
      });
      assert.equal(normalized.status(), 200, await normalized.text());
      // CPA hides a disabled runtime-only credential from metadata while keeping
      // its empty model set in the Host callback. It cannot own any model.
      runtimeSocket = new WebSocket(base.replace('http:', 'ws:') + '/v1/ws');
      await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('AIStudio fixture socket did not open')), 10000);
        runtimeSocket.addEventListener('open', () => { clearTimeout(timer); resolve(); }, { once: true });
        runtimeSocket.addEventListener('error', error => { clearTimeout(timer); reject(error); }, { once: true });
      });
      let runtimeCredential;
      for (let attempt = 0; attempt < 80; attempt++) {
        const files = await (await page.request.get(base + '/v8/management/credentials', { headers: managementHeaders })).json();
        runtimeCredential = files.files.find(file => file.provider === 'aistudio');
        if (runtimeCredential) break;
        await new Promise(resolve => setTimeout(resolve, 100));
      }
      assert(runtimeCredential, 'AIStudio runtime-only fixture did not register');
      const disabledRuntime = await page.request.patch(base + '/v8/management/credentials/status', {
        headers: managementHeaders, data: { name: runtimeCredential.id, auth_index: runtimeCredential.auth_index, disabled: true },
      });
      assert.equal(disabledRuntime.status(), 200, await disabledRuntime.text());
      const hiddenRuntime = await (await page.request.get(base + '/v8/management/credentials', { headers: managementHeaders })).json();
      assert(!hiddenRuntime.files.some(file => file.id === runtimeCredential.id), 'Disabled runtime-only credential must be absent from CPA metadata');
      await page.locator('#sync').click();
      await page.waitForFunction(() => !document.querySelector('#sync').disabled && !document.querySelector('#message').classList.contains('error'));
      synchronized = await readCatalog();
      assert.equal(synchronized.snapshot.credentials.length, inventory.length, 'A model-free Host entry must not create an owner or block the catalog');
      assert.match(await page.locator('#message').textContent(), /排除 1 个缺少元数据的无模型条目/);
      assert.match(await page.locator('#catalog-excluded').textContent(), /排除 1 个/);
      assert.equal(await page.locator('#catalog-excluded').getAttribute('hidden'), null);
      assert.doesNotMatch(JSON.stringify(synchronized.snapshot), /aistudio|auth_id|auth_index/);
      // A failed Host read must not publish a partial catalog.
      await page.route('**/v1/credential-models', route => route.fulfill({ status: 502, contentType: 'application/json', body: JSON.stringify({ error: 'fixture model sync failed' }) }));
      await page.locator('#sync').click();
      await page.waitForFunction(() => !document.querySelector('#sync').disabled && document.querySelector('#message').classList.contains('error'));
      assert.deepEqual((await readCatalog()).snapshot, synchronized.snapshot);
      await page.unroute('**/v1/credential-models');
      const hostModelURL = base + '/v0/management/plugins/cpa-helper-plugin/v1/credential-models';
      const hostModelFixture = await (await page.request.get(hostModelURL, { headers: managementHeaders })).json();
      const excludedRuntime = hostModelFixture.credentials.find(entry => entry.provider === 'aistudio');
      assert(excludedRuntime, 'Host callback must retain the disabled runtime-only entry');
      assert.deepEqual(excludedRuntime.models, []);
      let modelReads = 0;
      await page.route('**/v1/credential-models', route => {
        const result = structuredClone(hostModelFixture);
        if (++modelReads === 2) result.credentials[0].models.push('changed-during-sync');
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(result) });
      });
      await page.locator('#sync').click();
      await page.waitForFunction(() => !document.querySelector('#sync').disabled && document.querySelector('#message').classList.contains('error'));
      assert.equal(modelReads, 2);
      assert.deepEqual((await readCatalog()).snapshot, synchronized.snapshot, 'Model change during sync published a partial revision');
      await page.unroute('**/v1/credential-models');
      // Changes to entries excluded from the durable catalog still invalidate
      // the complete Host double read, including an empty -> owning transition.
      for (const change of ['auth_index', 'models']) {
        let reads = 0;
        await page.route('**/v1/credential-models', route => {
          const result = structuredClone(hostModelFixture);
          if (++reads === 2) {
            const entry = result.credentials.find(item => item.ref === excludedRuntime.ref);
            if (change === 'auth_index') entry.auth_index += '-changed';
            else entry.models.push('new-owner-during-sync');
          }
          return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(result) });
        });
        await page.locator('#sync').click();
        await page.waitForFunction(() => !document.querySelector('#sync').disabled && document.querySelector('#message').classList.contains('error'));
        assert.equal(reads, 2);
        assert.deepEqual((await readCatalog()).snapshot, synchronized.snapshot, 'Excluded Host ' + change + ' change published a partial revision');
        await page.unroute('**/v1/credential-models');
      }
      await page.locator('#sync').click();
      await page.waitForFunction(() => !document.querySelector('#sync').disabled && !document.querySelector('#message').classList.contains('error'));
      assert.equal(modelRefreshRequests, 0, 'Ordinary sync must not refresh model sources');
      const beforeRefresh = await readCatalog();
      await page.locator('[data-tab="health"]').click();
      await page.route('**/v8/management/models/refresh', route => route.fulfill({ status: 502, contentType: 'application/json', body: JSON.stringify({ ok: false, error: 'fixture refresh failed' }) }));
      await page.locator('#refresh-models').click();
      await page.waitForFunction(() => !document.querySelector('#refresh-models').disabled && document.querySelector('#message').classList.contains('error'));
      assert.deepEqual((await readCatalog()).snapshot, beforeRefresh.snapshot);
      await page.unroute('**/v8/management/models/refresh');
      await page.route('**/v8/management/models/refresh', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) }));
      await page.route('**/v1/credential-models', route => route.fulfill({ status: 502, contentType: 'application/json', body: JSON.stringify({ error: 'fixture sync failed after refresh' }) }));
      await page.locator('#refresh-models').click();
      await page.waitForFunction(() => !document.querySelector('#refresh-models').disabled && document.querySelector('#message').classList.contains('error'));
      assert.match(await page.locator('#message').textContent(), /已刷新.*尚未确认同步生效/);
      assert.deepEqual((await readCatalog()).snapshot, beforeRefresh.snapshot);
      await page.unroute('**/v1/credential-models');
      await page.unroute('**/v8/management/models/refresh');
      await page.locator('#refresh-models').click();
      await page.waitForFunction(() => !document.querySelector('#refresh-models').disabled && !document.querySelector('#message').classList.contains('error'));
      assert.equal(modelRefreshRequests, 3, 'One refresh request per explicit click');
      assert((await readCatalog()).snapshot.catalog_revision > beforeRefresh.snapshot.catalog_revision);
      assert.match(await page.locator('#message').textContent(), /排除 1 个缺少元数据的无模型条目/);
      assert.match(await page.locator('#catalog-excluded').textContent(), /排除 1 个/);
      assert.equal(await page.locator('#catalog-excluded').isVisible(), true);
      await page.locator('[data-tab="health"]').click();
      await mkdir(path.join(root, 'test-results'), { recursive: true });
      await page.screenshot({ path: path.join(root, 'test-results', 'health-desktop.png'), fullPage: true });
      await page.setViewportSize({ width: 390, height: 844 });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'Model-refresh health page overflows');
      await page.screenshot({ path: path.join(root, 'test-results', 'health-mobile.png'), fullPage: true });
      await page.setViewportSize({ width: 1440, height: 1000 });
      await page.locator('[data-tab="keys"]').click();
      assert.equal(await page.locator('#management-key, #login-form, #add-category, #category-provider').count(), 0);
      assert.match(await page.locator('#key-rows').textContent(), /previe\.\.\.lpha/);
      assert.equal(await page.locator('#save, #rollback').count(), 0);
      assert.equal(await page.locator('#add-model').count(), 0);
      const saved = () => page.waitForFunction(() => !document.querySelector('#editor').open);
      await page.locator('[data-tab="groups"]').click(); await page.locator('#add-group').click(); await page.locator('#edit-label').fill('Shared model access');
      await page.locator('#model-name').fill('restricted');
      assert.equal(await page.locator('#model-options select').count(), 1);
      await page.locator('#model-name').fill('');
      await page.locator('#edit-label').press('Tab');
      await page.locator('#model-options select[aria-label="demo-model"]').selectOption('allow');
      await page.locator('#model-options select[aria-label="restricted-model"]').selectOption('deny');
      await page.locator('#edit-note').fill('Independent route note');
      const readPolicy = async () => (await page.request.get(base + '/v0/management/plugins/cpa-helper-plugin/v1/policy', { headers: { Authorization: 'Bearer preview-management' } })).json();
      assert.equal((await readPolicy()).groups.length, 0, 'Edits saved before clicking Save');
      const editorLayout = await page.locator('#editor').evaluate(editor => {
        const dialog = editor.getBoundingClientRect();
        const body = editor.querySelector('.dialog-body').getBoundingClientRect();
        const bottom = editor.querySelector('.dialog-bottom').getBoundingClientRect();
        const buttons = [...editor.querySelectorAll('.dialog-footer button')].filter(button => button.getClientRects().length > 0).map(button => button.getBoundingClientRect());
        return {
          footerPosition: getComputedStyle(editor.querySelector('.dialog-footer')).position,
          bottomInsideDialog: bottom.left >= dialog.left && bottom.right <= dialog.right && bottom.bottom <= dialog.bottom + 1,
          bottomAfterBody: bottom.top >= body.bottom - 1,
          buttonsInsideBottom: buttons.every(button => button.left >= bottom.left - 1 && button.right <= bottom.right + 1 && button.bottom <= bottom.bottom + 1),
        };
      });
      assert.deepEqual(editorLayout, { footerPosition: 'static', bottomInsideDialog: true, bottomAfterBody: true, buttonsInsideBottom: true });
      await page.locator('#editor-form button[type="submit"]').click(); await saved();
      assert.equal((await readPolicy()).groups[0].note, 'Independent route note');
      assert.match(await page.locator('#group-list .group-note').textContent(), /Independent route note/);
      await page.locator('[data-tab="keys"]').click(); await page.locator('#key-rows button').first().click();
      assert.equal(await page.locator('#edit-key-value').textContent(), 'previe...lpha');
      assert.match(await page.locator('#credential-options').textContent(), /previe\.\.\.ream/);
      assert.doesNotMatch(await page.locator('#credential-options').textContent(), /preview-upstream/);
      assert.equal(await page.locator('#category-options select').count(), new Set(inventory.map(item => item.source + '/' + item.provider)).size);
      await page.locator('#edit-label').fill('Development key'); await page.locator('#edit-label').press('Tab');
      await page.locator('#edit-limit').fill('2'); await page.locator('#edit-limit').press('Tab');
      assert.equal(await page.locator('#edit-claude-ttl').inputValue(), '', 'Legacy Key must keep CPA behavior');
      await page.locator('#edit-claude-ttl').selectOption('1h');
      await page.locator('#bindings input').first().check();
      await page.locator('#credential-options select[aria-label*="previe...ream"]').selectOption('allow');
      assert.equal((await readPolicy()).keys.length, 0, 'Key edits saved before clicking Save');
      await page.locator('#editor-form button[type="submit"]').click(); await saved();
      const chat = model => page.request.post(base + '/v1/chat/completions', { headers: { Authorization: 'Bearer preview-key-alpha' }, data: { model, messages: [{ role: 'user', content: 'fixture' }] } });
      assert.equal((await readPolicy()).keys[0].claude_cache_ttl, '1h');
      await page.locator('#key-rows button').first().click();
      assert.equal(await page.locator('#edit-claude-ttl').inputValue(), '1h');
      await page.locator('#edit-claude-ttl').selectOption('5m');
      await page.locator('#editor-form button[type="submit"]').click(); await saved();
      assert.equal((await readPolicy()).keys[0].claude_cache_ttl, '5m');
      await page.locator('#key-rows button').first().click();
      await page.locator('#edit-claude-ttl').selectOption('');
      await page.locator('#editor-form button[type="submit"]').click(); await saved();
      assert.equal(Object.hasOwn((await readPolicy()).keys[0], 'claude_cache_ttl'), false);
      const permitted = await chat('demo-model');
      assert.equal(permitted.status(), 200, await permitted.text());
      const denied = await chat('restricted-model');
      assert.equal(denied.status(), 403, await denied.text());
      await page.reload(); await page.waitForFunction(() => !document.querySelector('#sync').disabled);
      assert.match(await page.locator('#key-rows').textContent(), /Development key/);
      assert.equal(await page.locator('#key-rows tr').first().locator('td strong').textContent(), 'Development key');
      assert.equal(await page.locator('#key-rows tr').first().locator('td small').textContent(), 'previe...lpha');
      assert.equal((await readPolicy()).groups[0].note, 'Independent route note');
      assert.doesNotMatch(await page.locator('#health-details').textContent(), /策略修订|上一修订/);
      assert.match(await page.locator('#key-rows').textContent(), /previe\.\.\.lpha/);
      assert.deepEqual(await page.evaluate(() => Object.keys(localStorage)), ['cli-proxy-auth']);
      assert.equal(await page.evaluate(() => sessionStorage.length), 0);
      const storedPolicy = await page.request.get(base + '/v0/management/plugins/cpa-helper-plugin/v1/policy', { headers: { Authorization: 'Bearer preview-management' } });
      assert.doesNotMatch(await storedPolicy.text(), /preview-key-alpha|preview-upstream/);
      assert.equal(await page.locator('.brand img').evaluate(img => img.complete && img.naturalWidth > 0), true);
      await mkdir(path.join(root, 'test-results'), { recursive: true });
      await page.screenshot({ path: path.join(root, 'test-results', 'desktop.png'), fullPage: true });
      await page.setViewportSize({ width: 390, height: 844 }); await page.screenshot({ path: path.join(root, 'test-results', 'mobile.png'), fullPage: true });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'mobile page overflows');
      await page.locator('#key-rows button').first().click(); await page.screenshot({ path: path.join(root, 'test-results', 'mobile-editor.png'), fullPage: true });
      assert.equal(await page.locator('#editor').evaluate(e => e.getBoundingClientRect().right <= innerWidth), true);
      assert.equal(await page.locator('#editor').evaluate(editor => {
        const dialog = editor.getBoundingClientRect();
        const body = editor.querySelector('.dialog-body');
        const bottom = editor.querySelector('.dialog-bottom').getBoundingClientRect();
        return body.scrollHeight > body.clientHeight && bottom.bottom <= dialog.bottom + 1 && bottom.top >= body.getBoundingClientRect().bottom - 1;
      }), true, 'mobile editor actions are not fixed inside the dialog');
      const policyURL = '**/v0/management/plugins/cpa-helper-plugin/v1/policy';
      const beforeRejectedSave = await readPolicy();
      let saveResponse = 'wrong-response-revision';
      let healthReads = 0;
      page.on('request', request => { if (request.method() === 'GET' && request.url().endsWith('/v0/management/plugins/cpa-helper-plugin/v1/health')) healthReads++; });
      await page.route(policyURL, route => {
        if (route.request().method() !== 'PUT') return route.continue();
        if (saveResponse === 'wrong-response-revision') return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ policy_revision: beforeRejectedSave.policy_revision + 2 }) });
        if (saveResponse === 'stale-health') return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ policy_revision: beforeRejectedSave.policy_revision + 1 }) });
        return route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":{"message":"fixture save failure"}}' });
      });
      await page.locator('#edit-limit').fill('3'); await page.locator('#edit-limit').press('Tab');
      await page.locator('#edit-claude-ttl').selectOption('1h');
      await page.locator('#editor-form button[type="submit"]').click();
      await page.waitForFunction(() => document.querySelector('#editor-error').textContent.includes('保存响应修订不一致'));
      assert.equal(healthReads, 0, 'health was read before the write response revision was accepted');
      saveResponse = 'stale-health';
      await page.locator('#editor-form button[type="submit"]').click();
      await page.waitForFunction(() => document.querySelector('#editor-error').textContent.includes('健康状态修订不一致'));
      assert.equal(healthReads, 1, 'health was not refreshed after an accepted write response');
      saveResponse = 'failure';
      await page.locator('#editor-form button[type="submit"]').click();
      await page.waitForFunction(() => document.querySelector('#editor-error').textContent.includes('fixture save failure'));
      assert.equal(await page.locator('#editor').evaluate(e => e.open), true);
      const unchanged = await page.request.get(base + '/v0/management/plugins/cpa-helper-plugin/v1/policy', { headers: { Authorization: 'Bearer preview-management' } });
      assert.equal((await unchanged.json()).keys[0].max_concurrency, 2);
      assert.equal(await page.locator('#edit-claude-ttl').inputValue(), '1h', 'Failed save lost TTL draft');
      await page.unroute(policyURL);
      await page.locator('#edit-limit').fill('2'); await page.locator('#edit-limit').press('Tab');
      await page.locator('#edit-claude-ttl').selectOption('');
      healthReads = 0;
      await page.locator('#editor-form button[type="submit"]').click(); await saved();
      assert.equal(healthReads, 1, 'successful save did not refresh health');
      await page.locator('[data-tab="groups"]').click();
      assert.equal(await page.locator('#group-list .group-bindings').textContent(), '1 个 Key 绑定');
      assert.equal(await page.locator('#group-list .group-note').textContent(), 'Independent route note');
      await page.locator('#group-list button').click(); await page.locator('#edit-note').fill('');
      await page.locator('#editor-form button[type="submit"]').click(); await saved();
      assert.equal((await readPolicy()).groups[0].note || '', '');
      await page.locator('#group-list button').click(); await page.locator('#edit-note').fill('Discard this');
      await page.locator('#close-editor').click(); await page.locator('#confirm button[value="ok"]').click(); await saved();
      assert.equal((await readPolicy()).groups[0].note || '', '');
      assert.equal(await page.locator('[data-tab="verification"], #verification-form, #save-verification').count(), 0);
      const capabilities = await page.request.get(base + '/v0/management/plugins/cpa-helper-plugin/v1/capabilities', { headers: managementHeaders });
      const caps = await capabilities.json();
      assert.equal('response_model_mismatch' in caps, false);
      assert.equal(caps.modules.includes('response_model_mismatch'), false);
      const removedRoute = await page.request.post(base + '/v0/management/plugins/cpa-helper-plugin/v1/response-model/validate', { headers: managementHeaders, data: { enabled: true } });
      assert.equal(removedRoute.status(), 404);
      const hostConfig = await page.request.get(base + '/v8/management/config/plugins/configs/cpa-helper-plugin', { headers: { Authorization: 'Bearer preview-management' } });
      assert.equal(hostConfig.status(), 200);
      const persistedConfig = await hostConfig.json();
      assert.deepEqual(persistedConfig.response_model_mismatch, { enabled: true, unknown_action: 'reject' }, 'Plugin rewrote inert legacy configuration');
      assert.equal(persistedConfig.enabled, true);
      assert.equal(persistedConfig.state_dir, posix(path.join(dir, 'state')));
      await page.reload();
      await page.waitForFunction(() => !document.querySelector('#sync').disabled);
      assert.equal(await page.locator('[data-tab="verification"], #verification-form').count(), 0);
      // Pin real v8 response shapes and exercise both sides of credential policy.
      const headerCredential = inventory.find(item => item.provider === 'claude');
      assert(headerCredential, 'header credential missing from the real CPA directory');
      for (const field of ['credential_ids', 'denied_credential_ids']) {
        const next = await readPolicy();
        const revision = next.policy_revision;
        next.policy_revision++; next.generated_at = new Date().toISOString();
        next.keys[0].group_ids = []; next.keys[0].rule = { [field]: [headerCredential.ref] };
        const published = await page.request.put(base + '/v0/management/plugins/cpa-helper-plugin/v1/policy', {
          headers: { ...managementHeaders, 'If-Match': String(revision), 'Idempotency-Key': 'header-contract-' + field }, data: next,
        });
        assert.equal(published.status(), 200, await published.text());
        const modelList = await page.request.get(base + '/v1/models', { headers: { Authorization: 'Bearer preview-key-alpha' } });
        assert.equal(modelList.status(), 200, await modelList.text());
        const ids = (await modelList.json()).data.map(model => model.id);
        if (field === 'credential_ids') assert.deepEqual(ids, ['header-model'], 'credential-only allow leaked unrelated models');
        else {
          assert(!ids.includes('header-model'), 'credential-only deny still advertised the model');
          assert(ids.includes('demo-model') && ids.includes('team/keyless-model'), 'deny hid unrelated public models');
          const hidden = await page.request.get(base + '/v1/models/header-model', { headers: { Authorization: 'Bearer preview-key-alpha' } });
          assert.equal(hidden.status(), 404, await hidden.text());
        }
        const before = claudeRequests;
        const response = await page.request.post(base + '/v1/messages', {
          headers: { Authorization: 'Bearer preview-key-alpha', 'anthropic-version': '2023-06-01' },
          data: { model: 'header-model', max_tokens: 10, messages: [{ role: 'user', content: 'header contract' }] },
        });
        if (field === 'credential_ids') {
          assert.equal(response.status(), 200, await response.text());
          assert.match(await response.text(), /Header fixture/);
          assert.equal(claudeRequests, before + 1);
        } else {
          assert.equal(response.status(), 503, await response.text());
          assert.match(await response.text(), /当前路由规则下没有可用的上游凭据/);
          assert.equal(claudeRequests, before, 'denied credential reached upstream');
        }
      }
      assert.deepEqual(hostRequests, [], 'UI called deprecated host management APIs');
      assert.deepEqual(errors, []);
      console.log('Browser smoke passed: session, masking, explicit save, request policy, reload, responsive layout, Host model sync with files/shared indexes/keyless/disabled credentials, model-free runtime-only exclusion with visible counts and complete Host double reads, exclusions/prefixes, atomic sync failures, explicit refresh stages, Key/TTL draft retention, removed response verification with inert legacy configuration and prototype-header allow/deny routing.');
    } finally { await browser.close(); }
    await cleanup();
  }
} catch (e) { await cleanup(); throw e; }
