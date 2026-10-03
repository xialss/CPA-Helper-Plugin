import { createServer } from 'node:http';
import { execFileSync, spawn } from 'node:child_process';
import { mkdtemp, mkdir, copyFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const trial = path.join(root, 'dist', 'response-model-trial');
const codex = process.env.CODEX_TEST_BINARY;
if (!codex) throw new Error('Set CODEX_TEST_BINARY to the Codex CLI executable');
const dir = await mkdtemp(path.join(tmpdir(), 'cpa-codex-retry-'));
const name = 'cpa-codex-retry-' + process.pid;
const docker = args => execFileSync('docker', args, { encoding: 'utf8', windowsHide: true }).trim();
const baseline = process.argv.includes('--baseline');
const leakBaseline = process.argv.includes('--leak-baseline');
let calls = 0, active = 0, peak = 0, canceled = 0, mode = 'mismatch';
const upstream = createServer((req, res) => {
  req.resume();
  req.on('end', () => {
    calls++; active++; peak = Math.max(peak, active);
    res.writeHead(200, { 'Content-Type': 'text/event-stream' });
    if (req.url === '/native/responses') {
      let sequence = 0;
      const send = (type, data) => res.write(`event: ${type}\ndata: ${JSON.stringify({ type, sequence_number: sequence++, ...data })}\n\n`);
      const model = mode === 'changed' || mode === 'matched' ? 'native-model' : '';
      send('response.created', { response: { id: 'resp_fixture', object: 'response', status: 'in_progress', model } });
      const item = mode === 'matched'
        ? { id: 'msg_fixture', type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text: 'verified-fixture-output', annotations: [] }] }
        : { id: 'tool_fixture', type: 'function_call', call_id: 'call_fixture', name: 'fixture_never_execute', arguments: '{"value":"private-tool-input"}', status: 'completed' };
      send('response.output_text.delta', { item_id: 'msg_fixture', output_index: 0, content_index: 0, delta: mode === 'matched' ? 'verified-fixture-output' : 'private-fixture-output' });
      send('response.output_item.done', { output_index: 0, item });
      const completion = setTimeout(() => {
        send('response.completed', { response: { id: 'resp_fixture', object: 'response', status: 'completed', model: mode === 'matched' ? 'native-model' : 'watered-model', output: [item], usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 } } });
        if (mode === 'matched') res.end();
      }, 300);
      const heartbeat = setInterval(() => res.write(': upstream-still-running\n\n'), 100);
      res.on('close', () => { clearTimeout(completion); clearInterval(heartbeat); active--; canceled++; });
      return;
    }
    const frame = model => 'data: ' + JSON.stringify({ id: 'fixture', object: 'chat.completion.chunk', model,
      choices: [{ index: 0, delta: { content: 'private-fixture-output' } }] }) + '\n\n';
    res.write(frame(mode === 'late' || mode === 'unknown' ? '' : 'watered-model'));
    const late = mode === 'late' ? setTimeout(() => res.write(frame('watered-model')), 100)
      : mode === 'unknown' ? setTimeout(() => res.write('data: {"id":"fixture","object":"chat.completion.chunk","model":"","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n'), 100) : null;
    // Keep the upstream alive: EOF must come from CPA cancellation, not the mock.
    const heartbeat = setInterval(() => res.write(': upstream-still-running\n\n'), 100);
    res.on('close', () => { clearInterval(heartbeat); if (late) clearTimeout(late); active--; canceled++; });
  });
});
await new Promise(resolve => upstream.listen(0, '0.0.0.0', resolve));
let started = false;
try {
  await mkdir(path.join(dir, 'plugins'));
  await mkdir(path.join(dir, 'client'));
  await copyFile(process.env.CPA_PLUGIN_BINARY || path.join(trial, 'cpa-helper-plugin.so'), path.join(dir, 'plugins', 'cpa-helper-plugin.so'));
  await copyFile(path.join(trial, 'cli-proxy-api'), path.join(dir, 'cli-proxy-api'));
  const settings = { enabled: true, stream_enabled: true, action: 'reject', unknown_action: 'pass', case_sensitive: false, ignore_thinking_suffix: true, ignored_models: [], accepted_models: {} };
  await writeFile(path.join(dir, 'config.yaml'), JSON.stringify({
    host: '0.0.0.0', port: 8317, 'auth-dir': '/fixture/auth', 'api-keys': ['fixture-downstream'],
    'remote-management': { 'secret-key': 'fixture-management', 'allow-remote': true, 'disable-control-panel': true, 'disable-auto-update-panel': true },
    'request-retry': 2, 'max-retry-interval': 1,
    plugins: { enabled: true, dir: '/fixture/plugins', configs: { 'cpa-helper-plugin': { enabled: true, state_dir: '/fixture/state', response_model_mismatch: settings } } },
    'openai-compatibility': [{ name: 'fixture', 'base-url': `http://host.docker.internal:${upstream.address().port}/v1`, 'api-key-entries': [{ 'api-key': 'fixture-upstream' }], models: [{ name: 'provider-model', alias: 'client-model' }] }],
    'codex-api-key': [{ 'api-key': 'fixture-native', 'base-url': `http://host.docker.internal:${upstream.address().port}/native`, models: [{ name: 'native-model', alias: 'native-model' }] }],
  }));
  docker(['run', '-d', '--name', name, '-p', '127.0.0.1::8317', '--mount', `type=bind,source=${dir},target=/fixture`, '-w', '/fixture', 'golang:1.26-bookworm', '/fixture/cli-proxy-api', '--config', '/fixture/config.yaml', '--local-model']);
  started = true;
  const base = 'http://127.0.0.1:' + docker(['port', name, '8317/tcp']).split(':').at(-1);
  const headers = { Authorization: 'Bearer fixture-management', 'Content-Type': 'application/json' };
  const capsURL = base + '/v0/management/plugins/cpa-helper-plugin/v1/capabilities';
  const waitFor = async (test, label) => {
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      if (await test()) return;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error('Timed out: ' + label);
  };
  await waitFor(async () => {
    try { return (await fetch(capsURL, { headers, signal: AbortSignal.timeout(1000) })).ok; }
    catch (e) { if (e.name === 'TimeoutError' || ['ECONNREFUSED', 'ECONNRESET', 'UND_ERR_SOCKET'].includes(e.cause?.code)) return false; throw e; }
  }, 'CPA startup');
  // Check wire output directly, including a complete tool item before the model
  // is declared. This must never be forwarded on a later mismatch.
  for (const nativeMode of (baseline ? [] : leakBaseline ? ['native-late'] : ['native-late', 'changed', 'matched'])) {
    mode = nativeMode;
    calls = 0; peak = 0; canceled = 0;
    const response = await fetch(base + '/v1/responses', { method: 'POST', headers: { Authorization: 'Bearer fixture-downstream', 'Content-Type': 'application/json', Originator: 'codex_cli_rs' }, body: JSON.stringify({ model: 'native-model', stream: true, input: 'synthetic tool leakage check' }), signal: AbortSignal.timeout(15000) });
    const wire = await response.text();
    if (mode === 'matched') {
      assert.match(wire, /verified-fixture-output/); assert.match(wire, /response.completed/); assert.doesNotMatch(wire, /invalid_prompt/);
    } else if (leakBaseline) {
      assert.match(wire, /private-tool-input/); assert.match(wire, /upstream_model_mismatch/);
    } else {
      assert.match(wire, /upstream_model_mismatch/); assert.doesNotMatch(wire, /private-fixture-output|private-tool-input|fixture_never_execute/);
    }
    await waitFor(() => active === 0, 'native upstream cancellation');
    assert.equal(calls, 1);
    console.log(JSON.stringify({ mode, leakBaseline, withheldTools: !wire.includes('private-tool-input'), upstreamRequests: calls, canceled }));
  }
  for (const nextMode of (leakBaseline ? [] : baseline ? ['mismatch'] : ['mismatch', 'late', 'unknown', 'native-late', 'changed'])) {
    mode = nextMode;
    settings.unknown_action = mode === 'unknown' ? 'reject' : 'pass';
    const saved = await fetch(base + '/v8/management/config/plugins/configs/cpa-helper-plugin/response_model_mismatch', { method: 'PUT', headers, body: JSON.stringify(settings) });
    assert.equal(saved.status, 200);
    await waitFor(async () => (await (await fetch(capsURL, { headers })).json()).response_model_mismatch.unknown_action === settings.unknown_action, 'configuration activation');
    calls = 0; peak = 0; canceled = 0;
    const native = mode === 'native-late' || mode === 'changed';
    // Native cases include a synthetic unknown tool. It must never reach Codex;
    // the isolated read-only client must see only the terminal policy error.
    const args = ['exec', '--ignore-user-config', '--ephemeral', '--skip-git-repo-check', '--sandbox', 'read-only', '--json', '-C', path.join(dir, 'client'),
      '-c', `model="${native ? 'native-model' : 'client-model'}"`, '-c', 'model_provider="fixture"',
      '-c', 'model_providers.fixture.name="fixture"', '-c', `model_providers.fixture.base_url="${base}/v1"`,
      '-c', 'model_providers.fixture.wire_api="responses"', '-c', 'model_providers.fixture.env_key="CPA_FIXTURE_KEY"',
      '-c', 'model_providers.fixture.stream_max_retries=2', '-c', 'model_providers.fixture.request_max_retries=2',
      'Fixture request. No tools.'];
    const env = Object.fromEntries(['PATH', 'SystemRoot', 'WINDIR', 'TEMP', 'TMP', 'USERPROFILE'].filter(k => process.env[k]).map(k => [k, process.env[k]]));
    env.CPA_FIXTURE_KEY = 'fixture-downstream';
    const child = spawn(codex, args, { cwd: path.join(dir, 'client'), env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
    let output = '', timedOut = false;
    child.stdout.on('data', b => { output += b; }); child.stderr.on('data', b => { output += b; });
    const timer = setTimeout(() => { timedOut = true; child.kill(); }, 30000);
    const exitCode = await new Promise((resolve, reject) => { child.on('error', reject); child.on('close', resolve); }).finally(() => clearTimeout(timer));
    assert.equal(timedOut, false, 'Codex did not terminate: ' + output);
    assert.notEqual(exitCode, 0, 'Rejected request reported success: ' + output);
    assert.match(output, baseline ? /上游响应模型不一致/ : /upstream_model_mismatch|response_model_unverifiable/, 'Missing policy rejection: ' + output);
    await waitFor(() => active === 0, 'upstream cancellation');
    if (baseline) assert(calls > 1, 'Baseline did not reproduce retries: ' + output);
    else {
      assert.equal(calls, 1, 'Codex retried policy rejection: ' + output); assert.equal(peak, 1); assert.equal(canceled, 1);
      assert.doesNotMatch(output, /Reconnecting/);
      assert.doesNotMatch(output, /private-fixture-output|private-tool-input|fixture_never_execute/);
    }
    console.log(JSON.stringify({ mode, baseline, upstreamRequests: calls, peakConcurrency: peak, canceled, clientExit: exitCode }));
  }
} finally {
  if (started) docker(['rm', '-f', name]);
  upstream.closeAllConnections();
  await new Promise(resolve => upstream.close(resolve));
  if (path.dirname(path.resolve(dir)) !== path.resolve(tmpdir()) || !path.basename(dir).startsWith('cpa-codex-retry-')) throw new Error('Unexpected fixture cleanup path');
  await rm(dir, { recursive: true, force: true });
}
