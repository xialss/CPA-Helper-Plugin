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
if (!binary) throw new Error('CPA_BINARY must point to CPA v8.0.12');
const serve = process.argv.includes('--serve');
const dir = await mkdtemp(path.join(tmpdir(), 'cpa-helper-ui-'));
await mkdir(path.join(dir, 'plugins'));
const ext = process.platform === 'win32' ? 'dll' : 'so';
await copyFile(path.join(root, 'dist', `cpa-helper-plugin.${ext}`), path.join(dir, 'plugins', `cpa-helper-plugin.${ext}`));
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
  host: '127.0.0.1', port, 'auth-dir': posix(path.join(dir, 'auth')),
  'api-keys': ['preview-key-alpha', 'preview-key-beta'],
  'remote-management': { 'secret-key': 'preview-management', 'allow-remote': false, 'disable-control-panel': true, 'disable-auto-update-panel': true },
  'request-retry': 0,
  plugins: { enabled: true, dir: posix(path.join(dir, 'plugins')), configs: { 'cpa-helper-plugin': { enabled: true, state_dir: posix(path.join(dir, 'state')) } } },
  'openai-compatibility': [
    { name: 'preview', 'base-url': `http://127.0.0.1:${upstream.address().port}/v1`, 'api-key-entries': [{ 'api-key': 'preview-upstream' }], models: [{ name: 'demo-model', alias: 'demo-model' }, { name: 'restricted-model', alias: 'restricted-model' }] },
    { name: 'keyless-null', 'base-url': `http://127.0.0.1:${upstream.address().port}/v1`, 'api-key-entries': null, models: [{ name: 'keyless-model', alias: 'keyless-model' }] },
  ],
  'claude-api-key': [{ 'api-key': 'header-fixture-key', 'base-url': `http://127.0.0.1:${upstream.address().port}`, headers: JSON.parse('{"__proto__":"fixture-header"}'), models: [{ name: 'header-model', alias: 'header-model' }] }],
};
await writeFile(path.join(dir, 'config.yaml'), JSON.stringify(config), { mode: 0o600 });
const log = createWriteStream(path.join(dir, 'cpa.log'));
const env = Object.fromEntries(['PATH', 'SystemRoot', 'WINDIR', 'TEMP', 'TMP', 'HOME'].filter(k => process.env[k]).map(k => [k, process.env[k]]));
const child = spawn(binary, ['--config', path.join(dir, 'config.yaml'), '--local-model'], { cwd: dir, env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
child.stdout.pipe(log); child.stderr.pipe(log);
let stopped = false;
async function cleanup() { if (stopped) return; stopped = true; if (child.exitCode === null) { const exit = new Promise(resolve => child.once('exit', resolve)); child.kill(); await exit; } await new Promise(resolve => upstream.close(resolve)); log.end(); }
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
      // CPA reads legacy null but its v8 write validator requires a list. Normalize
      // this isolated fixture only, so unrelated configuration-save tests can run.
      keyless.keys = [];
      const normalized = await page.request.put(base + '/v8/management/config/api-keys/openai-compatibility', {
        headers: managementHeaders, data: currentConfig['api-keys']['openai-compatibility'],
      });
      assert.equal(normalized.status(), 200, await normalized.text());
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
      assert.equal(await page.locator('#category-options select').count(), 3);
      await page.locator('#edit-label').fill('Development key'); await page.locator('#edit-label').press('Tab');
      await page.locator('#edit-limit').fill('2'); await page.locator('#edit-limit').press('Tab');
      await page.locator('#bindings input').first().check();
      await page.locator('#credential-options select[aria-label*="previe...ream"]').selectOption('allow');
      assert.equal((await readPolicy()).keys.length, 0, 'Key edits saved before clicking Save');
      await page.locator('#editor-form button[type="submit"]').click(); await saved();
      const chat = model => page.request.post(base + '/v1/chat/completions', { headers: { Authorization: 'Bearer preview-key-alpha' }, data: { model, messages: [{ role: 'user', content: 'fixture' }] } });
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
      await page.unroute(policyURL);
      await page.locator('#edit-limit').fill('2'); await page.locator('#edit-limit').press('Tab');
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
      await page.locator('[data-tab="verification"]').click();
      assert.equal(await page.locator('#verify-responses-only').count(), 0);
      assert.equal(await page.locator('#verify-buffer').inputValue(), '2');
      assert.equal(await page.locator('#verify-total-buffer').inputValue(), '8');
      await page.locator('#verify-buffer').fill('3');
      await page.locator('#verify-total-buffer').fill('2');
      await page.locator('#save-verification').click();
      await page.waitForFunction(() => document.querySelector('#verification-status').textContent.includes('不小于单请求上限'));
      await page.locator('#verify-total-buffer').fill('12');
      await page.locator('#verify-ignored').fill('v8-config-contract');
      await page.locator('#verify-accepted').fill('demo-model = upstream-model');
      await page.locator('#save-verification').click();
      await page.waitForFunction(() => document.querySelector('#verification-status').textContent === '已保存并生效');
      const hostConfig = await page.request.get(base + '/v8/management/config/plugins/configs/cpa-helper-plugin', { headers: { Authorization: 'Bearer preview-management' } });
      assert.equal(hostConfig.status(), 200);
      const persistedConfig = await hostConfig.json();
      assert.deepEqual(persistedConfig.response_model_mismatch.ignored_models, ['v8-config-contract']);
      assert.equal(persistedConfig.response_model_mismatch.max_buffer_bytes, 3 * 1024 * 1024);
      assert.equal(persistedConfig.response_model_mismatch.max_total_buffer_bytes, 12 * 1024 * 1024);
      assert.equal('responses_only_stream' in persistedConfig.response_model_mismatch, false);
      assert.equal(persistedConfig.enabled, true);
      assert.equal(persistedConfig.state_dir, posix(path.join(dir, 'state')));
      await page.reload();
      await page.waitForFunction(() => !document.querySelector('#sync').disabled);
      await page.locator('[data-tab="verification"]').click();
      assert.equal(await page.locator('#verify-ignored').inputValue(), 'v8-config-contract');
      assert.equal(await page.locator('#verify-buffer').inputValue(), '3');
      assert.equal(await page.locator('#verify-total-buffer').inputValue(), '12');
      assert.equal(await page.locator('#verify-accepted').inputValue(), 'demo-model = upstream-model');
      await page.locator('#verify-accepted').fill('');
      await page.locator('#save-verification').click();
      await page.waitForFunction(() => document.querySelector('#verification-status').textContent === '已保存并生效');
      const cleared = await page.request.get(base + '/v8/management/config/plugins/configs/cpa-helper-plugin', { headers: { Authorization: 'Bearer preview-management' } });
      assert.deepEqual((await cleared.json()).response_model_mismatch.accepted_models, {});
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
      console.log('Browser smoke passed: session, masking, explicit save, request policy, reload, binding count, responsive layout, null keyless list and prototype-header allow/deny routing.');
    } finally { await browser.close(); }
    await cleanup();
  }
} catch (e) { await cleanup(); throw e; }
