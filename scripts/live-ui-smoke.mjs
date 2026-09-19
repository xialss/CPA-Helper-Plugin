import { chromium } from 'playwright';
import assert from 'node:assert/strict';

const base = process.env.CPA_URL;
const key = process.env.CPA_MANAGEMENT_KEY;
const mask = value => value.length > 10 ? value.slice(0, 6) + '...' + value.slice(-4) : '***';
if (!base || !key) throw new Error('CPA_URL and CPA_MANAGEMENT_KEY are required');
const browser = await chromium.launch({ channel: 'msedge', headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.name));
  await page.goto(base + '/management.html');
  const input = page.locator('input[name="cpa-management-key"]');
  await input.fill(key);
  const remember = page.getByRole('checkbox', { name: /记住密码|Remember password/i });
  if (!await remember.isChecked()) await remember.press('Space');
  await input.press('Enter');
  await input.waitFor({ state: 'detached' });
  await page.goto(base + '/management.html#/plugin-pages/cpa-helper-plugin/0');
  const frame = page.frameLocator('iframe');
  await frame.locator('#workspace').waitFor({ state: 'visible' });
  await frame.locator('#message').filter({ hasText: 'CPA 目录已同步' }).waitFor();
  assert.equal(await frame.locator('#management-key, #login-form, #add-category, #save, #rollback').count(), 0);
  const response = await page.request.get(base + '/v0/management/api-keys', { headers: { Authorization: `Bearer ${key}` } });
  assert.equal(response.status(), 200);
  const rawKeys = (await response.json())['api-keys'];
  const rendered = await frame.locator('#key-rows').textContent();
  assert(rawKeys.every(value => rendered.includes(mask(value)) && !rendered.includes(value)), 'Downstream masking failed');
  const layout = await frame.locator('.keys-table').evaluate(table => {
    const cells = [...table.tBodies[0].rows[0].cells];
    const before = cells.map(c => c.getBoundingClientRect().width);
    const original = cells[2].textContent;
    cells[2].textContent = Array(30).fill('Long-rule-' + 'a'.repeat(180)).join('、');
    const after = cells.map(c => c.getBoundingClientRect().width);
    const contained = cells[2].scrollWidth <= cells[2].clientWidth;
    cells[2].textContent = original;
    return { before, after, contained, background: getComputedStyle(document.documentElement).backgroundColor,
      actionsBelowHeader: document.querySelector('.header-actions').getBoundingClientRect().top >= document.querySelector('header').getBoundingClientRect().bottom + 20 };
  });
  assert.deepEqual(layout.after, layout.before, 'Long rules changed column widths');
  assert(layout.contained, 'Long rule escaped its cell');
  assert(layout.before[1] >= 80 && layout.before[3] >= 110, 'Status/concurrency columns squeezed');
  assert.equal(layout.background, 'rgb(255, 255, 255)');
  assert(layout.actionsBelowHeader, 'Actions overlap host toolbar area');
  assert.doesNotMatch(await frame.locator('#health-details').textContent(), /策略修订|上一修订/);
  const activeBadge = frame.locator('.badge:not(.off)').first();
  if (await activeBadge.count()) assert.equal(await activeBadge.evaluate(e => getComputedStyle(e).backgroundColor), 'rgb(228, 241, 233)');
  await frame.locator('#key-rows button').first().click();
  const configResponse = await page.request.get(base + '/v0/management/config', { headers: { Authorization: `Bearer ${key}` } });
  assert.equal(configResponse.status(), 200);
  const config = await configResponse.json();
  const credentialText = await frame.locator('#credential-options').textContent();
  const providers = (config['openai-compatibility'] || []).filter(p => !p.disabled);
  for (const provider of providers) {
    assert(credentialText.includes(provider.name), 'Provider name missing');
    for (const entry of provider['api-key-entries'] || []) {
      const raw = entry['api-key'];
      if (raw) assert(credentialText.includes(mask(raw)) && !credentialText.includes(raw), 'Upstream masking failed');
    }
  }
  assert(await frame.locator('#category-options select').count() > 0);
  assert(!(await frame.locator('#category-options').textContent()).includes('API 提供方'));
  assert((await frame.locator('#category-options').textContent()).includes('API 提供商'));
  const policyEndpoint = base + '/v0/management/plugins/cpa-helper-plugin/v1/policy';
  const healthEndpoint = base + '/v0/management/plugins/cpa-helper-plugin/v1/health';
  const policyBefore = await (await page.request.get(policyEndpoint, { headers: { Authorization: `Bearer ${key}` } })).json();
  const healthBefore = await (await page.request.get(healthEndpoint, { headers: { Authorization: `Bearer ${key}` } })).json();
  const expectedRevision = policyBefore.policy_revision + 1;
  let saveResponse = 'wrong-response-revision';
  let healthReads = 0;
  page.on('request', request => { if (request.method() === 'GET' && request.url() === healthEndpoint) healthReads++; });
  await page.route(policyEndpoint, route => {
    if (route.request().method() !== 'PUT') return route.continue();
    const revision = saveResponse === 'wrong-response-revision' ? expectedRevision + 1 : expectedRevision;
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ policy_revision: revision }) });
  });
  await page.route(healthEndpoint, route => saveResponse === 'accepted'
    ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...healthBefore, policy_revision: expectedRevision, accepted_at: '2037-01-02T03:04:05Z', last_error: 'health-refresh-sentinel' }) })
    : route.continue());
  const currentLimit = await frame.locator('#edit-limit').inputValue();
  await frame.locator('#edit-limit').fill(currentLimit);
  await frame.locator('#editor-form button[type="submit"]').click();
  await frame.locator('#editor-error').filter({ hasText: '保存响应修订不一致' }).waitFor();
  assert.equal(healthReads, 0, 'Health was read before accepting the write revision');
  saveResponse = 'stale-health';
  await frame.locator('#editor-form button[type="submit"]').click();
  await frame.locator('#editor-error').filter({ hasText: '健康状态修订不一致' }).waitFor();
  assert.equal(healthReads, 1, 'Health was not refreshed after the accepted write response');
  saveResponse = 'accepted'; healthReads = 0;
  await frame.locator('#editor-form button[type="submit"]').click();
  await frame.locator('#editor').waitFor({ state: 'hidden' });
  assert.equal(healthReads, 1, 'Successful save did not refresh health');
  assert.match(await frame.locator('#health-details').textContent(), /health-refresh-sentinel/);
  await page.unroute(policyEndpoint); await page.unroute(healthEndpoint);
  const policyAfter = await (await page.request.get(policyEndpoint, { headers: { Authorization: `Bearer ${key}` } })).json();
  assert.equal(policyAfter.policy_revision, policyBefore.policy_revision, 'Simulated UI save reached the live policy');
  await frame.locator('[data-tab="groups"]').click();
  await frame.locator('#add-group').click();
  assert.equal(await frame.locator('#edit-note').isVisible(), true);
  assert.equal(await frame.locator('#edit-note').getAttribute('required'), null);
  assert.equal(await frame.locator('#editor-form button[type="submit"]').textContent(), '保存');
  await frame.locator('#close-editor').click();
  await page.reload();
  await page.frameLocator('iframe').locator('#message').filter({ hasText: 'CPA 目录已同步' }).waitFor();
  assert.deepEqual(errors, []);
  console.log('Live CPA panel: layout, session reuse, masking, write revision checks and health refresh passed. No policy writes.');
} finally {
  await browser.close();
}
