import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { Readable } from 'node:stream';
import test from 'node:test';
import { runSettings } from '../settings.mjs';
import { main, parseArgs } from '../mediactl.mjs';

const cookie = 'a'.repeat(43);
const csrf = 'b'.repeat(43);
const privateText = 'PRIVATE_R18_METADATA';

async function fixture(handler) {
    const calls = [];
    const app = createServer(async (req, res) => {
        let body = '';
        for await (const chunk of req) body += chunk;
        calls.push({ method: req.method, path: req.url, headers: req.headers, body });
        if (req.url === '/api/auth/session') return respond(res, { authenticated: true, csrf_token: csrf, expires_at: 'future' });
        if (req.url === '/api/client-contract') return respond(res, { protocol_version: 1, client: 'mediactl' });
        await handler(req, res, body);
    });
    app.listen(0, '127.0.0.1');
    await once(app, 'listening');
    const origin = `http://127.0.0.1:${app.address().port}`;
    const store = { load: async () => ({ origin, cookie, csrf, expiresAt: 'future' }), save: async () => {} };
    return { app, calls, origin, store };
}

function respond(res, data, status = 200) {
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(data));
}

const options = (server, more = {}) => ({ store: server.store, allowInsecureLoopback: true, ...more });
const input = (data) => ({ inputJson: '-', stdin: Readable.from([JSON.stringify(data)]) });

test('settings commands parse three levels and route with safe output', async () => {
    assert.deepEqual(parseArgs(['settings', 'ai', 'models', '--config-version', '4']).subaction, 'models');
    assert.equal(parseArgs(['connections', 'telegram', 'cancel', '--attempt-id', 'a'.repeat(32)]).attemptId, 'a'.repeat(32));
    const output = [];
    const code = await main(['--json', 'settings', 'download', 'get'], {
        stdout: { write: (value) => output.push(value) }, stderr: { write: () => {} },
        runSettingsCommand: async (section, action) => ({ section, action }),
    });
    assert.equal(code, 0);
    assert.deepEqual(JSON.parse(output[0]).data, { section: 'download', action: 'get' });
});

test('download settings preserve versioned JSON, and conflict does not overwrite local input', async (t) => {
    const saved = { timeout: 20, retries: 2, image_concurrency: 4, download_action_mode: 'prompt', config_version: 3 };
    let conflict = false;
    const server = await fixture((req, res) => {
        if (req.url !== '/api/settings/download') return respond(res, {}, 404);
        if (req.method === 'PUT' && conflict) return respond(res, { code: 'config_conflict', message: privateText }, 409);
        return respond(res, saved);
    });
    t.after(() => server.app.close());
    assert.deepEqual(await runSettings('download', 'get', options(server)), saved);
    const update = { expected_version: 3, timeout: 20, retries: 2, image_concurrency: 4, download_action_mode: 'prompt' };
    const first = input(update);
    await runSettings('download', 'set', options(server, { inputJson: first.inputJson }), { stdin: first.stdin });
    const write = server.calls.find((call) => call.method === 'PUT');
    assert.equal(write.body, JSON.stringify(update));
    assert.equal(write.headers.origin, server.origin);
    assert.equal(write.headers['x-csrf-token'], csrf);
    conflict = true;
    const second = input(update);
    await assert.rejects(runSettings('download', 'set', options(server, { inputJson: second.inputJson }), { stdin: second.stdin }), (error) => {
        assert.equal(error.code, 'config_conflict');
        assert.doesNotMatch(error.message, new RegExp(privateText));
        return true;
    });
    assert.equal(update.expected_version, 3);
});

test('source and AI credentials require independent TTY before any session or mutation', async () => {
    const store = { load: async () => { throw new Error('session must not be read'); } };
    for (const [section, provider] of [['sources', 'openlibrary'], ['ai', undefined]]) {
        await assert.rejects(runSettings(section, 'set', { store, provider, credential: 'replace', json: true }), { code: 'interaction_required' });
        await assert.rejects(runSettings(section, 'set', { store, provider, credential: 'replace', interactive: false }), { code: 'interaction_required' });
    }
});

test('setting reviews require an independent TTY and keep values out of machine results', async (t) => {
    const forbiddenStore = { load: async () => { throw new Error('session must not be read'); } };
    for (const section of ['fields', 'rules', 'ai', 'sources']) {
        const provider = section === 'sources' ? 'catalog' : undefined;
        await assert.rejects(runSettings(section, 'review', { store: forbiddenStore, provider, json: true }), { code: 'interaction_required' });
        await assert.rejects(runSettings(section, 'review', { store: forbiddenStore, provider }, { interactive: false }), { code: 'interaction_required' });
    }
    const credentialSentinel = 'DO_NOT_PRINT_API_KEY';
    const targetURL = 'https://saved-model.example/v1';
    const server = await fixture((req, res) => {
        if (req.url === '/api/settings/metadata-fields') return respond(res, {
            definitions_version: 'v1', limits: { custom_fields: 64 },
            definitions: [{ key: 'custom.user.note', label: privateText, type: 'string', enabled: true, editable: true, extractable: ['rule'] }],
        });
        if (req.url === '/api/settings/extraction-rules') return respond(res, {
            rules_version: 2, definitions_version: 'v1',
            rules: [{ id: 'note', target_key: 'custom.user.note', labels: [privateText], mode: 'label_value' }],
        });
        if (req.url === '/api/settings/ai') return respond(res, {
            enabled: true, base_url: targetURL, model_id: 'mini-cpm', credential_configured: true,
            config_version: 4, api_key: credentialSentinel,
        });
        if (req.url === '/api/settings/sources') return respond(res, { sources: [{
            provider_id: 'catalog', enabled: true, priority: 7, auth_mode: 'bearer', credential_configured: true,
            config_version: 3, filters: { rating: 'all' }, field_preferences: { title: 'original' },
            credential: credentialSentinel, descriptor: { label: '书目来源', capabilities: ['search'] },
        }] });
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const reviewed = [];
    const fields = await runSettings('fields', 'review', options(server), { interactive: true, reviewer: async (value) => reviewed.push(JSON.stringify(value)) });
    const rules = await runSettings('rules', 'review', options(server), { interactive: true, reviewer: async (value) => reviewed.push(JSON.stringify(value)) });
    const ai = await runSettings('ai', 'review', options(server), { interactive: true, reviewer: async (value) => reviewed.push(JSON.stringify(value)) });
    const source = await runSettings('sources', 'review', options(server, { provider: 'catalog' }), { interactive: true, reviewer: async (value) => reviewed.push(JSON.stringify(value)) });
    assert.equal(fields.reviewed, true);
    assert.equal(rules.reviewed, true);
    assert.equal(ai.reviewed, true);
    assert.equal(source.reviewed, true);
    assert.equal(reviewed.length, 4);
    assert.ok(reviewed.slice(0, 2).every((value) => value.includes(privateText)));
    assert.ok(reviewed[2].includes(targetURL));
    assert.ok(reviewed[3].includes('"rating":"all"'));
    assert.doesNotMatch(JSON.stringify(reviewed), new RegExp(credentialSentinel));
    assert.doesNotMatch(JSON.stringify({ fields, rules, ai, source }), new RegExp(`${privateText}|${targetURL}|${credentialSentinel}`));
    assert.equal(server.calls.some((call) => call.method !== 'GET'), false);
});

test('source settings and model probes return bounded summaries without credential or upstream text', async (t) => {
    const server = await fixture((req, res, body) => {
        if (req.url === '/api/settings/sources') return respond(res, { sources: [{ provider_id: 'catalog', enabled: true, priority: 1, auth_mode: 'bearer', credential_configured: true, config_version: 2, filters: {}, field_preferences: {}, descriptor: { capabilities: ['search'], auth_modes: ['bearer'] } }] });
        if (req.url === '/api/settings/sources/catalog' && req.method === 'PUT') {
            assert.equal(JSON.parse(body).credential.value, privateText);
            return respond(res, { provider_id: 'catalog', enabled: true, priority: 1, auth_mode: 'bearer', credential_configured: true, config_version: 3, filters: {}, field_preferences: {} });
        }
        if (req.url === '/api/settings/sources/catalog/test') return respond(res, { provider_id: 'catalog', config_version: 3, status: 'passed', scope: 'public_catalog_only', checked_at: '2026-10-05T00:00:00Z', upstream: privateText });
        if (req.url === '/api/settings/ai' && req.method === 'GET') return respond(res, { enabled: true, base_url: `https://${privateText}.example/v1`, model_id: 'minicpm', credential_configured: true, config_version: 4 });
        if (req.url === '/api/settings/ai/models') return respond(res, { config_version: 4, ignored: 1, models: [{ id: 'minicpm', capability: 'unknown', selectable: true, secret: privateText }], upstream: privateText });
        if (req.url === '/api/settings/ai/test') return respond(res, { config_version: 4, model_id: 'minicpm', status: 'passed', field_keys: ['title'], fixture_version: 'v1', prompt: privateText });
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const sources = await runSettings('sources', 'list', options(server));
    const update = input({ expected_version: 2, enabled: true, priority: 1, filters: {}, field_preferences: {} });
    const result = await runSettings('sources', 'set', options(server, { provider: 'catalog', credential: 'replace', interactive: true, inputJson: update.inputJson }), { stdin: update.stdin, secretReader: async () => privateText });
    const tested = await runSettings('sources', 'test', options(server, { provider: 'catalog', configVersion: '3' }));
    const ai = await runSettings('ai', 'get', options(server));
    const models = await runSettings('ai', 'models', options(server, { configVersion: '4' }));
    const aiTest = await runSettings('ai', 'test', options(server, { configVersion: '4', model: 'minicpm' }));
    assert.equal(sources.sources[0].credential_configured, true);
    assert.equal(result.config_version, 3);
    assert.equal(tested.scope, 'public_catalog_only');
    assert.equal(ai.target_configured, true);
    assert.equal(ai.protocol, 'llama_cpp_native');
    assert.equal(models.models[0].id, 'minicpm');
    assert.equal(aiTest.field_key_count, 1);
    assert.doesNotMatch(JSON.stringify({ sources, result, tested, ai, models, aiTest }), new RegExp(privateText));
});

test('rules preview runs locally and exposes field keys without matched text', async (t) => {
    const rules = { rules_version: 1, definitions_version: 'v1', rules: [{ id: 'title', target_key: 'title', labels: ['标题'], mode: 'label_value' }] };
    const schema = { schema_version: 1, definitions_version: 'v1', definitions: { title: { key: 'title', label: '标题', type: 'string', enabled: true, extractable: ['rule'] } } };
    const server = await fixture((req, res) => {
        if (req.url === '/api/metadata/schema') return respond(res, schema);
        if (req.url === '/api/settings/extraction-rules') return respond(res, rules);
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const preview = input({ text: `标题：${privateText}` });
    const result = await runSettings('rules', 'preview', options(server, { inputJson: preview.inputJson }), { stdin: preview.stdin });
    assert.equal(result.candidate_count, 1);
    assert.deepEqual(result.candidates[0].field_keys, ['title']);
    assert.doesNotMatch(JSON.stringify(result), new RegExp(privateText));
    assert.equal(server.calls.some((call) => call.method !== 'GET'), false);
    assert.equal(server.calls.some((call) => call.body.includes(privateText)), false);
});

test('AI, field and rule updates use server CAS and expose only safe configuration summaries', async (t) => {
    const fields = { expected_definitions_version: 'v1', definitions: [{ key: 'custom.user.note', label: privateText, type: 'string', enabled: true, extractable: ['rule'], editable: true, export_status: 'internal_only' }] };
    const rules = { expected_version: 2, definitions_version: 'v1', rules: [{ id: 'note', target_key: 'custom.user.note', labels: [privateText], mode: 'label_value' }] };
    const server = await fixture((req, res, body) => {
        if (req.url === '/api/settings/ai' && req.method === 'PUT') {
            assert.deepEqual(JSON.parse(body), { expected_version: 4, enabled: true, base_url: 'https://model.example/v1', model_id: 'minicpm', credential: { action: 'clear' } });
            return respond(res, { config_version: 5, enabled: true, base_url: 'https://model.example/v1', model_id: 'minicpm', credential_configured: false });
        }
        if (req.url === '/api/settings/metadata-fields' && req.method === 'PUT') {
            assert.equal(body, JSON.stringify(fields));
            return respond(res, { definitions_version: 'v2', definitions: fields.definitions, limits: { custom_fields: 64 } });
        }
        if (req.url === '/api/settings/extraction-rules' && req.method === 'PUT') {
            assert.equal(body, JSON.stringify(rules));
            return respond(res, { rules_version: 3, definitions_version: 'v1', rules: rules.rules });
        }
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const aiInput = input({ expected_version: 4, enabled: true, base_url: 'https://model.example/v1', model_id: 'minicpm' });
    const ai = await runSettings('ai', 'set', options(server, { inputJson: aiInput.inputJson, credential: 'clear' }), { stdin: aiInput.stdin });
    const fieldsInput = input(fields);
    const definitions = await runSettings('fields', 'set', options(server, { inputJson: fieldsInput.inputJson }), { stdin: fieldsInput.stdin });
    const rulesInput = input(rules);
    const ruleSet = await runSettings('rules', 'set', options(server, { inputJson: rulesInput.inputJson }), { stdin: rulesInput.stdin });
    assert.equal(ai.config_version, 5);
    assert.equal(ai.protocol, 'llama_cpp_native');
    assert.equal(definitions.definitions[0].key, 'custom.user.note');
    assert.equal(ruleSet.rules[0].label_count, 1);
    assert.doesNotMatch(JSON.stringify({ ai, definitions, ruleSet }), new RegExp(privateText));
});

test('AI protocol is sent explicitly and returned in safe summaries and terminal reviews', async (t) => {
    const target = 'https://cpa.example/v1';
    const server = await fixture((req, res, body) => {
        if (req.url !== '/api/settings/ai') return respond(res, {}, 404);
        if (req.method === 'PUT') assert.deepEqual(JSON.parse(body), { expected_version: 4, enabled: true, protocol: 'llama_cpp_chat', base_url: target, model_id: 'minicpm', credential: { action: 'keep' } });
        return respond(res, { config_version: 5, enabled: true, protocol: 'llama_cpp_chat', base_url: target, model_id: 'minicpm', credential_configured: true, api_key: privateText });
    });
    t.after(() => server.app.close());
    const update = input({ expected_version: 4, enabled: true, protocol: 'llama_cpp_chat', base_url: target, model_id: 'minicpm' });
    const result = await runSettings('ai', 'set', options(server, { inputJson: update.inputJson }), { stdin: update.stdin });
    const readback = await runSettings('ai', 'get', options(server));
    let review;
    await runSettings('ai', 'review', options(server), { interactive: true, reviewer: async value => { review = value; } });
    assert.equal(result.protocol, 'llama_cpp_chat');
    assert.equal(readback.protocol, 'llama_cpp_chat');
    assert.equal(review.protocol, 'llama_cpp_chat');
    assert.equal(review.base_url, target);
    assert.doesNotMatch(JSON.stringify({ result, readback }), new RegExp(`${target}|${privateText}`));
    assert.doesNotMatch(JSON.stringify(review), new RegExp(privateText));
});

test('AI rejects invalid explicit protocols before writes or credential prompts', async (t) => {
    const server = await fixture((_req, res) => respond(res, {}, 500));
    t.after(() => server.app.close());
    for (const protocol of [null, '', 'openai', 42]) {
        const update = input({ expected_version: 4, enabled: true, protocol, base_url: 'https://cpa.example/v1', model_id: 'minicpm' });
        await assert.rejects(runSettings('ai', 'set', options(server, { inputJson: update.inputJson, credential: 'replace' }), {
            stdin: update.stdin, interactive: true, secretReader: async () => { throw new Error('must not prompt for invalid configuration'); },
        }), { code: 'invalid_input' });
    }
    assert.equal(server.calls.some(call => call.method === 'PUT'), false);
});

test('AI summaries reject an unknown or null saved protocol', async (t) => {
    let protocol;
    const server = await fixture((_req, res) => respond(res, { config_version: 1, protocol }));
    t.after(() => server.app.close());
    for (protocol of [null, '', 'openai']) await assert.rejects(runSettings('ai', 'get', options(server)), { code: 'invalid_response' });
});
