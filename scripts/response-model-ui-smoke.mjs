import { chromium } from 'playwright';
import { createServer } from 'node:http';
import { execFileSync } from 'node:child_process';
import { mkdtemp, mkdir, copyFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const trial = path.join(root, 'dist', 'response-model-trial');
const dir = await mkdtemp(path.join(tmpdir(), 'cpa-response-ui-'));
const name = 'cpa-response-ui-' + process.pid;
const docker = args => execFileSync('docker', args, { encoding: 'utf8', windowsHide: true }).trim();
const upstream = createServer((req, res) => {
  req.resume();
  req.on('end', () => {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ id: 'ui-fixture', object: 'chat.completion', model: 'watered-model', choices: [{ index: 0, message: { role: 'assistant', content: 'must-not-leak' }, finish_reason: 'stop' }] }));
  });
});
await new Promise(resolve => upstream.listen(0, '0.0.0.0', resolve));
let browser, started = false;
try {
  await mkdir(path.join(dir, 'plugins'));
  await copyFile(path.join(trial, 'cpa-helper-plugin.so'), path.join(dir, 'plugins', 'cpa-helper-plugin.so'));
  await copyFile(path.join(trial, 'cli-proxy-api'), path.join(dir, 'cli-proxy-api'));
  await writeFile(path.join(dir, 'config.yaml'), JSON.stringify({
    host: '0.0.0.0', port: 8317, 'auth-dir': '/fixture/auth',
    'api-keys': ['fixture-downstream'],
    'remote-management': { 'secret-key': 'fixture-management', 'allow-remote': true, 'disable-control-panel': true, 'disable-auto-update-panel': true },
    'request-retry': 0,
    plugins: { enabled: true, dir: '/fixture/plugins', configs: { 'cpa-helper-plugin': { enabled: true, state_dir: '/fixture/state' } } },
    'openai-compatibility': [{ name: 'fixture', 'base-url': `http://host.docker.internal:${upstream.address().port}/v1`, 'api-key-entries': [{ 'api-key': 'fixture-upstream' }], models: [{ name: 'provider-model', alias: 'client-model' }] }],
  }));
  docker(['run', '-d', '--name', name, '-p', '127.0.0.1::8317', '--mount', `type=bind,source=${dir},target=/fixture`, '-w', '/fixture', 'golang:1.26-bookworm', '/fixture/cli-proxy-api', '--config', '/fixture/config.yaml', '--local-model']);
  started = true;
  const port = docker(['port', name, '8317/tcp']).split(':').at(-1);
  let base = 'http://127.0.0.1:' + port;
  const headers = { Authorization: 'Bearer fixture-management' };
  let capsURL = base + '/v0/management/plugins/cpa-helper-plugin/v1/capabilities';
  const waitReady = async () => {
    const deadline = Date.now() + 30000;
    while (Date.now() < deadline) {
      try { const r = await fetch(capsURL, { headers, signal: AbortSignal.timeout(1000) }); if (r.ok) return; }
      catch (e) { if (e.name !== 'TimeoutError' && !['ECONNREFUSED', 'ECONNRESET', 'UND_ERR_SOCKET'].includes(e.cause?.code)) throw e; }
      await new Promise(resolve => setTimeout(resolve, 150));
    }
    throw new Error('CPA startup failed: ' + docker(['logs', name]));
  };
  await waitReady();
  browser = await chromium.launch({ channel: 'msedge', headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  page.on('dialog', d => d.accept());
  await page.addInitScript(() => localStorage.setItem('managementKey', 'fixture-management'));
  const ui = base + '/v0/resource/plugins/cpa-helper-plugin/ui';
  await page.goto(ui);
  await page.waitForFunction(() => !document.querySelector('#refresh').disabled);
  await page.locator('[data-tab="verification"]').click();
  assert.equal(await page.locator('#verify-enabled').isChecked(), false);
  await page.locator('#verify-enabled').check();
  await page.locator('#verify-unknown').selectOption('reject');
  await page.locator('#verify-ignored').fill('auto');
  await page.locator('#verify-accepted').fill('trusted-alias = trusted-model, trusted-model-2026');
  await page.locator('#save-verification').click();
  await page.waitForFunction(() => document.querySelector('#verification-status').textContent === '已保存并生效');
  const settings = (await (await fetch(capsURL, { headers })).json()).response_model_mismatch;
  assert.equal(settings.enabled, true); assert.equal(settings.unknown_action, 'reject');
  assert.deepEqual(settings.accepted_models, { 'trusted-alias': ['trusted-model', 'trusted-model-2026'] });
  const response = await fetch(base + '/v1/chat/completions', { method: 'POST', headers: { Authorization: 'Bearer fixture-downstream', 'Content-Type': 'application/json' }, body: JSON.stringify({ model: 'client-model', messages: [{ role: 'user', content: 'UI acceptance' }] }) });
  assert.equal(response.headers.get('X-CPA-Helper-Model-Mismatch'), 'true');
  const body = await response.text();
  assert.match(body, /upstream_model_mismatch/); assert.doesNotMatch(body, /must-not-leak/);
  await mkdir(path.join(root, 'test-results'), { recursive: true });
  await page.screenshot({ path: path.join(root, 'test-results', 'response-model-desktop.png'), fullPage: true });
  await page.reload();
  await page.waitForFunction(() => !document.querySelector('#refresh').disabled);
  await page.locator('[data-tab="verification"]').click();
  assert.equal(await page.locator('#verify-enabled').isChecked(), true);
  await page.locator('#verify-accepted').fill('malformed rule');
  await page.locator('#save-verification').click();
  await page.waitForFunction(() => document.querySelector('#message.error')?.textContent.includes('映射格式'));
  assert.match(await page.locator('#verification-status').textContent(), /未确认生效/);
  assert.deepEqual((await (await fetch(capsURL, { headers })).json()).response_model_mismatch, settings);
  await page.locator('#refresh').click();
  await page.locator('#confirm button[value="cancel"]').click();
  assert.equal(await page.locator('#verify-accepted').inputValue(), 'malformed rule');
  await page.locator('#refresh').click();
  await page.locator('#confirm button[value="ok"]').click();
  await page.waitForFunction(() => !document.querySelector('#refresh').disabled);
  assert.equal(await page.locator('#verification-status').textContent(), '');
  assert.equal(await page.locator('#verify-accepted').inputValue(), 'trusted-alias = trusted-model, trusted-model-2026');
  await page.locator('[data-tab="verification"]').click();
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.screenshot({ path: path.join(root, 'test-results', 'response-model-mobile.png'), fullPage: true });
  docker(['restart', name]);
  const restartedPort = docker(['port', name, '8317/tcp']).split(':').at(-1);
  console.log('Docker restart port mapping: ' + port + ' -> ' + restartedPort);
  base = 'http://127.0.0.1:' + restartedPort;
  capsURL = base + '/v0/management/plugins/cpa-helper-plugin/v1/capabilities';
  await waitReady();
  assert.deepEqual((await (await fetch(capsURL, { headers })).json()).response_model_mismatch, settings);
  assert.deepEqual(errors, []);
  console.log('Response model UI passed: real CPA, save, validation, reload, restart persistence, HTTP error/header, desktop/mobile.');
} finally {
  if (browser) await browser.close();
  if (started) docker(['rm', '-f', name]);
  await new Promise(resolve => upstream.close(resolve));
  if (path.dirname(path.resolve(dir)) !== path.resolve(tmpdir()) || !path.basename(dir).startsWith('cpa-response-ui-')) throw new Error('Unexpected fixture cleanup path');
  await rm(dir, { recursive: true, force: true });
}
