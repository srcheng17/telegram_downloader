import assert from 'node:assert/strict';
import { test } from 'node:test';
import http from 'node:http';
import { Readable } from 'node:stream';
import { mkdtemp, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { createWorkspaceState } from '../workspace_state.mjs';
import { runWorkspace, runWorkspaceJSONL } from '../workspace.mjs';
import { main, parseArgs } from '../mediactl.mjs';
import { EXIT } from '../errors.mjs';
import { createSessionStore } from '../session_store.mjs';
import { startWorkspace, workspaceRPC } from '../workspace_broker.mjs';

const secret = 'Private R18 Synthetic Reveal 9f6e';
const imageFixture = new URL('../../tests/e2e/fixtures/ocr/eng.png', import.meta.url).pathname;
const schema = {
    schema_version: 1,
    definitions_version: 'reveal-v1',
    limits: { document_bytes: 262144, provenance_per_field: 8 },
    definitions: {
        title: { key: 'title', label: '标题', type: 'string', enabled: true, editable: true, extractable: ['rule', 'ai', 'provider'], max_bytes: 1024 },
    },
};
const ruleSet = { definitions_version: 'reveal-v1', rules_version: 1, rules: [{ id: 'title', target_key: 'title', labels: ['Title'], mode: 'label_value' }] };

function fixtureState({ now = () => Date.now(), post = async () => { throw new Error('unexpected POST'); } } = {}) {
    const get = async (path) => {
        if (path === '/api/metadata/schema') return schema;
        if (path === '/api/settings/extraction-rules') return ruleSet;
        if (path === '/api/settings/ai') return { enabled: true, model_id: 'saved-model', base_url: 'https://ai.example.invalid/v1', config_version: 1 };
        throw new Error(`unexpected GET: ${path}`);
    };
    return createWorkspaceState({ schema, get, post, now, recognizer: { recognize: async () => secret, close: async () => {} } });
}

test('reveal is single use, short lived, revision bound, and independent of AI review', async () => {
    let time = 1000;
    const state = fixtureState({ now: () => time });
    try {
        await state.apply('text-set', { text: secret });
        const revision = state.revision;
        const prepared = await state.apply('reveal-prepare', { kind: 'merged' }, revision);
        assert.match(prepared.ticket, /^[a-f0-9-]{36}$/u);
        assert.equal(prepared.kind, 'merged');
        assert.equal(prepared.bytes, Buffer.byteLength(JSON.stringify({ text: secret })));
        assert.ok(!JSON.stringify(prepared).includes(secret));
        await assert.rejects(state.apply('reveal-consume', { ticket: prepared.ticket }, revision - 1), { code: 'workspace_conflict' });
        const shown = await state.apply('reveal-consume', { ticket: prepared.ticket }, revision);
        assert.deepEqual(shown.content, { text: secret });
        await assert.rejects(state.apply('reveal-consume', { ticket: prepared.ticket }, revision), { code: 'workspace_conflict' });

        const stale = await state.apply('reveal-prepare', { kind: 'merged' }, revision);
        await state.apply('field-set', { key: 'title', value: secret });
        await assert.rejects(state.apply('reveal-consume', { ticket: stale.ticket }, state.revision), { code: 'workspace_conflict' });

        const expiring = await state.apply('reveal-prepare', { kind: 'field', id: 'title' }, state.revision);
        time += 61_000;
        await assert.rejects(state.apply('reveal-consume', { ticket: expiring.ticket }, state.revision), { code: 'workspace_conflict' });
        const field = await state.apply('reveal-prepare', { kind: 'field', id: 'title' }, state.revision);
        const fieldValue = await state.apply('reveal-consume', { ticket: field.ticket }, state.revision);
        assert.deepEqual(fieldValue.content, { state: 'value', value: secret });

        await state.apply('ai-prepare', { keys: ['title'] });
        const ai = await state.apply('ai-review');
        await assert.rejects(state.apply('reveal-consume', { ticket: ai.ticket }, state.revision), { code: 'workspace_conflict' });
    } finally { await state.close(); }
});

test('reveal narrows candidate and record content to the selected item', async () => {
    const state = fixtureState({ post: async (path, body) => {
        if (path === '/api/metadata/search') return { query_revision: body.query_revision, sources: [{ provider_id: 'bangumi', config_version: 1,
            candidates: [{ record_id: 'record-1', title: secret, aliases: ['Other'], creators: { author: ['One'] }, relationship: 'series', public_url: 'https://example.invalid/record-1' }] }] };
        throw new Error(`unexpected POST: ${path}`);
    } });
    try {
        await state.apply('text-set', { text: `Title: ${secret}` });
        const rules = await state.apply('rules-preview');
        const candidateId = rules.candidates[0].candidate_id;
        const ticket = await state.apply('reveal-prepare', { kind: 'candidate', id: candidateId }, state.revision);
        assert.ok(!JSON.stringify(ticket).includes(secret));
        const candidate = await state.apply('reveal-consume', { ticket: ticket.ticket }, state.revision);
        assert.deepEqual(candidate.content.fields.title, { state: 'value', value: secret });
        assert.equal(candidate.id, candidateId);
        assert.equal(candidate.content.provenance, undefined);

        const search = await state.apply('provider-search', { keyword: secret, providerIds: ['bangumi'] });
        const resultId = search.results[0].result_id;
        const recordTicket = await state.apply('reveal-prepare', { kind: 'record', id: resultId }, state.revision);
        const record = await state.apply('reveal-consume', { ticket: recordTicket.ticket }, state.revision);
        assert.equal(record.id, resultId);
        assert.equal(record.content.title, secret);
        assert.equal(record.content.public_url, undefined);
        await assert.rejects(state.apply('reveal-prepare', { kind: 'record', id: 'missing' }, state.revision), { code: 'invalid_input' });
    } finally { await state.close(); }
});

test('image reveal returns only the selected edited OCR text', async () => {
    const state = fixtureState();
    try {
        const added = await state.apply('image-add', { paths: [imageFixture] });
        const id = added.images[0].image_id;
        for (let attempt = 0; attempt < 100 && state.status().images[0].status !== 'done'; attempt++) {
            await new Promise((resolve) => setTimeout(resolve, 2));
        }
        assert.equal(state.status().images[0].status, 'done');
        const prepared = await state.apply('reveal-prepare', { kind: 'image', id }, state.revision);
        assert.ok(!JSON.stringify(prepared).includes(secret));
        const shown = await state.apply('reveal-consume', { ticket: prepared.ticket }, state.revision);
        assert.deepEqual(shown.content, { text: secret });
        assert.equal(shown.id, id);
    } finally { await state.close(); }
});

test('private broker enforces full revision on single-use reveal tickets', async () => {
    const root = await mkdtemp('/tmp/mw-reveal-');
    const server = http.createServer((request, response) => {
        response.setHeader('Content-Type', 'application/json');
        if (request.url === '/api/auth/session') response.end(JSON.stringify({ authenticated: true, csrf_token: 'c'.repeat(43), expires_at: '2030-01-01T00:00:00Z' }));
        else if (request.url === '/api/metadata/schema') response.end(JSON.stringify(schema));
        else { response.statusCode = 404; response.end('{}'); }
    });
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const origin = 'http://127.0.0.1:' + server.address().port;
    const store = createSessionStore(join(root, 'config'));
    const directory = join(store.directory, 'workspaces');
    await store.save({ origin, cookie: 'a'.repeat(43), csrf: 'c'.repeat(43) });
    let id;
    try {
        const started = await startWorkspace({ server: origin, allowInsecureLoopback: true }, { directory, store });
        id = started.workspace_id;
        const set = await workspaceRPC(id, { op: 'text-set', args: { text: secret }, expectedRevision: 0 }, { directory });
        const prepared = await workspaceRPC(id, { op: 'reveal-prepare', args: { kind: 'merged' }, expectedRevision: set.revision }, { directory });
        assert.ok(!JSON.stringify(prepared).includes(secret));
        await assert.rejects(workspaceRPC(id, { op: 'reveal-consume', args: { ticket: prepared.ticket }, expectedRevision: 0 }, { directory }), { code: 'workspace_conflict' });
        const shown = await workspaceRPC(id, { op: 'reveal-consume', args: { ticket: prepared.ticket }, expectedRevision: set.revision }, { directory });
        assert.equal(shown.content.text, secret);
        await assert.rejects(workspaceRPC(id, { op: 'reveal-consume', args: { ticket: prepared.ticket }, expectedRevision: set.revision }, { directory }), { code: 'workspace_conflict' });
    } finally {
        if (id) {
            const status = await workspaceRPC(id, { op: 'status' }, { directory }).catch(() => null);
            if (status) await workspaceRPC(id, { op: 'close', expectedRevision: status.revision }, { directory }).catch(() => {});
        }
        await new Promise((resolve) => server.close(resolve));
        await rm(root, { recursive: true, force: true });
    }
});

test('CLI requires both explicit JSON and AI-context flags before reveal consume IPC', async () => {
    const prepareArgs = ['workspace', 'reveal', 'prepare', '--workspace', 'a'.repeat(32), '--revision', '3', '--kind', 'field', '--id', 'title'];
    let prepareRequest;
    const prepared = await runWorkspace('reveal', 'prepare', parseArgs(prepareArgs), {
        rpc: async (_id, request) => {
            prepareRequest = request;
            return { ticket: 'a'.repeat(36), revision: 3, kind: 'field', bytes: 100, expires_in_seconds: 60 };
        },
    });
    assert.deepEqual(prepareRequest, { op: 'reveal-prepare', args: { kind: 'field', id: 'title' }, expectedRevision: 3 });
    assert.ok(!JSON.stringify(prepared).includes(secret));
    const prepareOut = []; const prepareErr = [];
    const prepareCode = await main(prepareArgs, {
        stdout: { write: (value) => prepareOut.push(value) }, stderr: { write: (value) => prepareErr.push(value) },
        runWorkspaceCommand: async () => prepared,
    });
    assert.equal(prepareCode, 0);
    assert.match(prepareOut.join(''), /reveal 票据/u);
    assert.ok(!prepareOut.join('').includes(secret));
    assert.deepEqual(prepareErr, []);

    const base = ['workspace', 'reveal', 'consume', '--workspace', 'a'.repeat(32), '--revision', '3', '--ticket', '1'.repeat(36)];
    let calls = 0;
    const rpc = async () => { calls++; return { kind: 'merged', content: { text: secret } }; };
    for (const args of [base, ['--json', ...base], [...base, '--to-ai-context']]) {
        const options = parseArgs(args);
        await assert.rejects(runWorkspace('reveal', 'consume', options, { rpc }), { code: 'interaction_required' });
    }
    assert.equal(calls, 0);
    const options = parseArgs(['--json', ...base, '--to-ai-context']);
    const content = await runWorkspace('reveal', 'consume', options, { rpc });
    assert.equal(content.content.text, secret);
    assert.equal(calls, 1);

    const out = []; const err = [];
    const code = await main(['--json', ...base, '--to-ai-context'], {
        stdout: { write: (value) => out.push(value) }, stderr: { write: (value) => err.push(value) },
        runWorkspaceCommand: async (action, subaction, parsed) => runWorkspace(action, subaction, parsed, { rpc }),
    });
    assert.equal(code, 0);
    assert.equal(JSON.parse(out.join('')).data.content.text, secret);
    assert.deepEqual(err, []);

    const safeOut = []; const safeErr = [];
    const blocked = await main(base, {
        stdout: { write: (value) => safeOut.push(value) }, stderr: { write: (value) => safeErr.push(value) },
        runWorkspaceCommand: async (action, subaction, parsed) => runWorkspace(action, subaction, parsed, { rpc }),
    });
    assert.equal(blocked, EXIT.interaction);
    assert.ok(!safeOut.join('').includes(secret));
    assert.ok(!safeErr.join('').includes(secret));
    assert.equal(calls, 2);

    const blockedOut = []; const blockedErr = [];
    const blockedJson = await main(['--json', ...base], {
        stdout: { write: (value) => blockedOut.push(value) }, stderr: { write: (value) => blockedErr.push(value) },
        runWorkspaceCommand: async () => { calls++; throw new Error(secret); },
    });
    assert.equal(blockedJson, EXIT.interaction);
    assert.equal(blockedOut.length, 0);
    assert.equal(JSON.parse(blockedErr.join('')).code, 'interaction_required');
    assert.ok(!blockedErr.join('').includes(secret));
    assert.equal(calls, 2);
});

test('JSONL rejects reveal operations without broker calls or sensitive output', async () => {
    const output = []; let calls = 0;
    await runWorkspaceJSONL({ workspace: 'a'.repeat(32) }, {
        stdin: Readable.from([
            `${JSON.stringify({ op: 'reveal-prepare', expected_revision: 1, args: { kind: 'merged' } })}\n`,
            `${JSON.stringify({ op: 'reveal-consume', expected_revision: 1, args: { ticket: secret } })}\n`,
        ]),
        stdout: { write: (value) => { output.push(value); return true; } },
        rpc: async () => { calls++; throw new Error(secret); },
    });
    assert.equal(calls, 0);
    assert.ok(!output.join('').includes(secret));
    assert.deepEqual(output.map((value) => JSON.parse(value).code), ['invalid_input', 'invalid_input']);
});
