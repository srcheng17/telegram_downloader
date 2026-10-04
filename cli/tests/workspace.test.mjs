import assert from 'node:assert/strict';
import { test } from 'node:test';
import http from 'node:http';
import { Readable } from 'node:stream';
import { mkdtemp, rm, symlink, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createWorkspaceState } from '../workspace_state.mjs';
import { readImageFile, createNodeRecognizer, readClipboardImage } from '../ocr.mjs';
import { parseArgs, main } from '../mediactl.mjs';
import { EXIT } from '../errors.mjs';
import { createSessionStore } from '../session_store.mjs';
import { startWorkspace, workspaceRPC } from '../workspace_broker.mjs';
import { runWorkspace, runWorkspaceJSONL } from '../workspace.mjs';

const fixture = new URL('../../tests/e2e/fixtures/ocr/eng.png', import.meta.url).pathname;
const schema = {
    schema_version: 1, definitions_version: 'test-v1', limits: { document_bytes: 262144, provenance_per_field: 8 },
    definitions: {
        title: { key: 'title', label: '标题', type: 'string', enabled: true, editable: true, extractable: ['rule', 'ai', 'provider', 'legacy'], max_bytes: 1024 },
        tags: { key: 'tags', label: '标签', type: 'string[]', enabled: true, editable: true, extractable: ['rule', 'ai', 'provider', 'legacy'], max_items: 64, item_max_bytes: 1024 },
    },
};
const ruleSet = { definitions_version: 'test-v1', rules_version: 1, rules: [{ id: 'title', target_key: 'title', labels: ['Title'], mode: 'label_value' }] };
const secret = 'Private R18 Synthetic Title';

function fixtureState({ recognizer = { recognize: async () => `Title: ${secret}`, close: async () => {} }, getOverride, postOverride, submit } = {}) {
    const get = async (path) => {
        if (getOverride) return getOverride(path);
        if (path === '/api/metadata/schema') return schema;
        if (path === '/api/settings/extraction-rules') return ruleSet;
        if (path === '/api/settings/ai') return { enabled: true, model_id: 'saved-model', base_url: 'https://ai.example.invalid/v1', config_version: 3 };
        if (path === '/api/settings/sources') return { sources: [{ provider_id: 'bangumi', enabled: true, config_version: 2 }] };
        if (path === '/api/metadata/providers') return { sources: [{ provider_id: 'bangumi', enabled: true, config_version: 2 }] };
        if (path.startsWith('/api/metadata-history')) return [];
        throw new Error(path);
    };
    const post = async (path, body) => {
        if (postOverride) return postOverride(path, body);
        if (path === '/api/metadata/validate') return { document: body.document, warnings: [] };
        throw new Error(path);
    };
    return createWorkspaceState({ schema, get, post, recognizer, submit });
}

async function waitForImage(state) {
    for (let attempt = 0; attempt < 100; attempt++) {
        if (state.status().images[0]?.status === 'done') return;
        await new Promise((resolve) => setTimeout(resolve, 10));
    }
    assert.fail('OCR did not finish');
}

test('workspace accepts several images in order, keeps raw and edited text private', async () => {
    const state = fixtureState();
    try {
        const result = await state.apply('image-add', { paths: [fixture, fixture] });
        assert.equal(result.images.length, 2);
        await waitForImage(state);
        for (let attempt = 0; attempt < 100 && state.status().images[1]?.status !== 'done'; attempt++) await new Promise((resolve) => setTimeout(resolve, 10));
        assert.equal(state.status().images[1].status, 'done');
        assert.ok(!JSON.stringify(state.status()).includes(secret));
        assert.equal(state.status().images.length, 2);
        const first = state.status().images[0].image_id;
        await state.apply('image-edit', { imageId: first, text: `Title: edited ${secret}` });
        const review = await state.apply('review');
        assert.match(review.images[0].raw_text, /Private R18/);
        assert.match(review.images[0].edited_text, /edited Private R18/);
        assert.match(review.merged.text, /Private R18/);
        assert.ok(review.merged.warnings.includes('overlapping_text') === false);
        await state.apply('image-move', { imageId: first, direction: 'down' });
        assert.equal(state.status().images[1].image_id, first);
        await state.apply('image-remove', { imageId: first });
        assert.equal(state.status().images.length, 1);
    } finally { await state.close(); }
});

test('rule candidates stay unapplied and require fresh authority before adoption', async () => {
    let currentRules = ruleSet;
    const state = fixtureState({ getOverride: async (path) => {
        if (path === '/api/metadata/schema') return schema;
        if (path === '/api/settings/extraction-rules') return currentRules;
        throw new Error(path);
    } });
    try {
        await state.apply('text-set', { text: `Title: ${secret}` });
        const preview = await state.apply('rules-preview');
        assert.equal(preview.candidate_count, 1);
        assert.ok(!JSON.stringify(preview).includes(secret));
        assert.equal(state.status().field_keys.length, 0);
        const candidateId = preview.candidates[0].candidate_id;
        const comparison = await state.apply('candidate-preview', { candidateId });
        assert.equal(comparison.fields[0].key, 'title');
        assert.ok(!JSON.stringify(comparison).includes(secret));
        currentRules = { ...ruleSet, rules_version: 2 };
        await assert.rejects(state.apply('candidate-adopt', { candidateId, keys: ['title'] }), { code: 'workspace_conflict' });
        assert.equal(state.status().field_keys.length, 0);
        currentRules = ruleSet;
        const adopted = await state.apply('candidate-adopt', { candidateId, keys: ['title'] });
        assert.deepEqual(adopted.adopted_keys, ['title']);
        assert.equal((await state.apply('review')).document.fields.title.value, secret);
    } finally { await state.close(); }
});

test('AI preview is private; extraction needs one-use reviewed ticket', async () => {
    let sent = 0;
    const state = fixtureState({ postOverride: async (path, body) => {
        assert.equal(path, '/api/metadata/extract'); sent++;
        assert.equal(body.text, `Title: ${secret}`);
        return { request_id: body.request_id, candidates: [], warnings: [{ code: 'no_evidence' }] };
    } });
    try {
        await state.apply('text-set', { text: `Title: ${secret}` });
        const prepared = await state.apply('ai-prepare', { keys: ['title'] });
        assert.equal(prepared.review_required, true);
        assert.ok(!JSON.stringify(prepared).includes(secret));
        await assert.rejects(state.apply('ai-extract', { ticket: 'bad' }), { code: 'workspace_conflict' });
        assert.equal(sent, 0);
        const review = await state.apply('ai-review');
        assert.equal(review.text, `Title: ${secret}`);
        const extracted = await state.apply('ai-extract', { ticket: review.ticket });
        assert.equal(sent, 1);
        assert.deepEqual(extracted.warning_codes, [{ code: 'no_evidence' }]);
        assert.ok(!JSON.stringify(extracted).includes(secret));
        await assert.rejects(state.apply('ai-extract', { ticket: review.ticket }), { code: 'workspace_conflict' });
    } finally { await state.close(); }
});

test('provider search and history expose only opaque IDs until terminal review', async () => {
    const state = fixtureState({
        getOverride: async (path) => {
            if (path === '/api/metadata/schema') return schema;
            if (path === '/api/settings/sources') return { sources: [{ provider_id: 'bangumi', enabled: true, config_version: 2 }] };
            if (path.startsWith('/api/metadata-history')) return [{ task_id: 'task-1', metadata_document: { schema_version: 1, definitions_version: 'test-v1', revision: 1, fields: { title: { state: 'value', value: secret, revision: 1 } }, definition_snapshot: { title: schema.definitions.title } } }];
            throw new Error(path);
        },
        postOverride: async (path, body) => {
            if (path === '/api/metadata/search') return { query_revision: body.query_revision, sources: [{ provider_id: 'bangumi', config_version: 2, error: { code: secret }, candidates: [{ record_id: 'record-1', title: secret, relationship: 'series', format: secret }] }] };
            if (path === '/api/metadata/candidates/resolve') return { config_version: 2, candidate: {
                candidate_id: 'candidate-1', request_id: 'request-1', origin: 'provider', schema_version: 1, definitions_version: 'test-v1',
                base_document_revision: body.base_document_revision, input_revision: body.query_revision, field_revisions: { title: 0 },
                fields: { title: { state: 'value', value: secret, provenance: [{ kind: 'provider', source_id: 'bangumi', record_id: 'record-1' }] } },
            } };
            throw new Error(path);
        },
    });
    try {
        const search = await state.apply('provider-search', { keyword: secret, providerIds: ['bangumi'] });
        assert.equal(search.result_count, 1);
        assert.equal(search.sources[0].error_code, 'unknown');
        assert.equal(search.results[0].format, 'unknown');
        assert.ok(!JSON.stringify(search).includes(secret));
        assert.equal((await state.apply('review')).results[0].record.title, secret);
        const resolved = await state.apply('provider-resolve', { resultId: search.results[0].result_id });
        assert.equal(resolved.candidate_count, 1);
        assert.ok(!JSON.stringify(resolved).includes(secret));
        const history = await state.apply('history-list');
        assert.equal(history.entries.length, 1);
        assert.ok(!JSON.stringify(history).includes(secret));
        assert.equal((await state.apply('review')).history[0].submitted_title, secret);
        const selected = await state.apply('history-select', { entryId: history.entries[0].entry_id });
        assert.equal(selected.candidate_count, 1);
        assert.ok(!JSON.stringify(selected).includes(secret));
    } finally { await state.close(); }
});

test('history candidate adoption rejects a changed server field definition', async () => {
    let currentSchema = schema;
    const state = fixtureState({ getOverride: async (path) => {
        if (path === '/api/metadata/schema') return currentSchema;
        if (path.startsWith('/api/metadata-history')) return [{ task_id: 'task-1', metadata_document: {
            schema_version: 1, definitions_version: 'test-v1', revision: 1,
            fields: { title: { state: 'value', value: secret, revision: 1 } },
            definition_snapshot: { title: schema.definitions.title },
        } }];
        throw new Error(path);
    } });
    try {
        const history = await state.apply('history-list');
        const selected = await state.apply('history-select', { entryId: history.entries[0].entry_id });
        currentSchema = { ...schema, definitions_version: 'test-v2' };
        await assert.rejects(state.apply('candidate-adopt', {
            candidateId: selected.candidates[0].candidate_id, keys: ['title'],
        }), { code: 'workspace_conflict' });
        assert.deepEqual(state.status().field_keys, []);
    } finally { await state.close(); }
});

test('workspace submits only its adopted document after schema and validation checks', async () => {
    let currentSchema = schema;
    let validationCount = 0;
    const submissions = [];
    const state = fixtureState({
        getOverride: async (path) => {
            if (path === '/api/metadata/schema') return currentSchema;
            if (path === '/api/settings/extraction-rules') return ruleSet;
            throw new Error(path);
        },
        postOverride: async (path, body) => {
            assert.equal(path, '/api/metadata/validate'); validationCount++;
            return { document: body.document, warnings: [{ key: 'title', code: 'internal_only', message: secret }] };
        },
        submit: async (action, args, document) => {
            submissions.push({ action, args, document });
            return { task: { id: 'task-1', status: 'READY' }, created: args.force === true, duplicate: args.force !== true, needs_confirmation: args.force !== true };
        },
    });
    try {
        await state.apply('text-set', { text: `Title: ${secret}` });
        const preview = await state.apply('rules-preview');
        await state.apply('candidate-adopt', { candidateId: preview.candidates[0].candidate_id, keys: ['title'] });
        const before = state.status();
        const duplicate = await state.apply('task-create-url', { url: 'https://telegra.ph/example' });
        assert.equal(duplicate.snapshot_attached, false);
        assert.equal(duplicate.revision, before.revision);
        assert.equal(duplicate.needs_confirmation, true);
        assert.equal(submissions[0].document.fields.title.value, secret);
        assert.equal(submissions[0].document.revision, before.document_revision);
        assert.ok(!JSON.stringify(duplicate).includes(secret));
        const created = await state.apply('task-create-url', { url: 'https://telegra.ph/example', force: true });
        assert.equal(created.snapshot_attached, true);
        assert.equal(created.revision, before.revision + 1);
        assert.equal(state.status().last_submission.task_id, 'task-1');
        assert.equal(validationCount, 2);
        currentSchema = { ...schema, definitions_version: 'test-v2' };
        await assert.rejects(state.apply('task-create-url', { url: 'https://telegra.ph/example' }), { code: 'workspace_conflict' });
        assert.equal(submissions.length, 2);
        assert.equal((await state.apply('review')).document.fields.title.value, secret);
    } finally { await state.close(); }
});

test('workspace keeps draft after upload failure and requires explicit partial image submission', async () => {
    let rejectUpload = true;
    const submissions = [];
    const state = fixtureState({
        recognizer: { recognize: () => new Promise(() => {}), close: async () => {} },
        submit: async (action, args, document) => {
            submissions.push({ action, args, document });
            if (rejectUpload) throw Object.assign(new Error('incomplete'), { code: 'upload_incomplete', taskId: 'task-1' });
            return { task: { id: 'task-2', status: 'READY' }, upload_complete: true };
        },
    });
    try {
        await state.apply('field-set', { key: 'title', value: secret });
        await state.apply('image-add', { paths: [fixture] });
        await assert.rejects(state.apply('task-upload', { file: '/tmp/sample.cbz' }), { code: 'workspace_conflict' });
        assert.equal(submissions.length, 0);
        const before = state.status();
        await assert.rejects(state.apply('task-upload', { file: '/tmp/sample.cbz', acceptPartial: true }), { code: 'upload_incomplete' });
        assert.equal(state.status().revision, before.revision);
        assert.equal((await state.apply('review')).document.fields.title.value, secret);
        rejectUpload = false;
        const uploaded = await state.apply('task-upload', { file: '/tmp/sample.cbz', acceptPartial: true });
        assert.equal(uploaded.snapshot_attached, true);
        assert.equal(uploaded.task.id, 'task-2');
        assert.ok(!JSON.stringify(uploaded).includes(secret));
    } finally { await state.close(); }
});

test('workspace refuses submission when OCR changes its revision during validation', async () => {
    let finishOCR; let releaseValidation; let validationStarted;
    const reachedValidation = new Promise((resolve) => { validationStarted = resolve; });
    const holdValidation = new Promise((resolve) => { releaseValidation = resolve; });
    let submissions = 0;
    const state = fixtureState({
        recognizer: { recognize: () => new Promise((resolve) => { finishOCR = resolve; }), close: async () => {} },
        postOverride: async (path, body) => {
            assert.equal(path, '/api/metadata/validate');
            validationStarted(); await holdValidation;
            return { document: body.document, warnings: [] };
        },
        submit: async () => { submissions++; return { task: { id: 'task-1', status: 'READY' }, created: true }; },
    });
    try {
        await state.apply('field-set', { key: 'title', value: secret });
        await state.apply('image-add', { paths: [fixture] });
        const submitting = state.apply('task-create-url', { url: 'https://telegra.ph/example', acceptPartial: true });
        await reachedValidation;
        finishOCR(`Title: ${secret}`);
        for (let attempt = 0; attempt < 20 && state.status().images[0].status !== 'done'; attempt++) await new Promise((resolve) => setTimeout(resolve, 1));
        assert.equal(state.status().images[0].status, 'done');
        releaseValidation();
        await assert.rejects(submitting, { code: 'workspace_conflict' });
        assert.equal(submissions, 0);
        assert.equal((await state.apply('review')).document.fields.title.value, secret);
    } finally { releaseValidation?.(); await state.close(); }
});

test('local Node OCR uses bundled resources and checks decoded image dimensions', async () => {
    const image = await readImageFile(fixture);
    const recognizer = createNodeRecognizer();
    try {
        const result = await recognizer.recognize(image.bytes, image.info, ['eng']);
        assert.match(result, /Star Atlas/);
        await assert.rejects(recognizer.recognize(image.bytes, { ...image.info, width: image.info.width + 1 }, ['eng']), { code: 'ocr_failed' });
    } finally { await recognizer.close(); }
});

test('OCR cancellation interrupts worker initialization and retires late worker', async () => {
    const image = await readImageFile(fixture);
    let finishCreation; let terminated = 0;
    const recognizer = createNodeRecognizer({ workerFactory: () => new Promise((resolve) => { finishCreation = resolve; }) });
    const pending = recognizer.recognize(image.bytes, image.info, ['eng']);
    await Promise.resolve();
    await recognizer.close();
    await assert.rejects(pending, { code: 'ocr_failed' });
    finishCreation({ terminate: async () => { terminated++; } });
    await new Promise((resolve) => setTimeout(resolve, 0));
    assert.equal(terminated, 1);
});

test('workspace parser preserves repeated image order and JSON AI call blocks before IPC', async () => {
    assert.deepEqual(parseArgs(['workspace', 'image', 'add', '--image', 'one.png', '--image', 'two.png']).images, ['one.png', 'two.png']);
    const submitOptions = parseArgs(['workspace', 'tasks', 'create-url', '--workspace', 'a'.repeat(32), '--revision', '3', '--url', 'https://telegra.ph/example', '--force']);
    let request;
    const submitted = await runWorkspace(submitOptions.action, submitOptions.subaction, submitOptions, {
        rpc: async (_id, value) => { request = value; return { task: { id: 'task-1' }, revision: 4 }; },
    });
    assert.equal(submitted.task.id, 'task-1');
    assert.deepEqual(request, { op: 'task-create-url', args: { url: 'https://telegra.ph/example', force: true, acceptPartial: false }, expectedRevision: 3 });
    const structured = { ...submitOptions, url: undefined, inputJson: '-' };
    await runWorkspace('tasks', 'create-url', structured, {
        stdin: Readable.from([JSON.stringify({ url: 'https://telegra.ph/example', force: true, accept_partial: true })]),
        rpc: async (_id, value) => { request = value; return { task: { id: 'task-2' }, revision: 4 }; },
    });
    assert.deepEqual(request.args, { url: 'https://telegra.ph/example', force: true, acceptPartial: true });
    await runWorkspace('tasks', 'create-url', { workspace: submitOptions.workspace, revision: '3', inputJson: '-' }, {
        stdin: Readable.from([JSON.stringify({ url: 'https://telegra.ph/example' })]),
        rpc: async (_id, value) => { request = value; return { task: { id: 'task-default' }, revision: 4 }; },
    });
    assert.deepEqual(request.args, { url: 'https://telegra.ph/example', force: false, acceptPartial: false });
    await runWorkspace('tasks', 'upload', { workspace: submitOptions.workspace, revision: '3', inputJson: '-' }, {
        stdin: Readable.from([JSON.stringify({ file: '/tmp/sample.cbz', accept_partial: true })]),
        rpc: async (_id, value) => { request = value; return { task: { id: 'task-3' }, revision: 4 }; },
    });
    assert.deepEqual(request.args, { file: '/tmp/sample.cbz', acceptPartial: true });
    let rejectedCalls = 0;
    await assert.rejects(runWorkspace('tasks', 'create-url', { ...structured, force: false }, {
        stdin: Readable.from([JSON.stringify({ url: 'https://telegra.ph/example', force: true })]),
        rpc: async () => { rejectedCalls++; },
    }), { code: 'invalid_input' });
    await assert.rejects(runWorkspace('tasks', 'upload', { workspace: submitOptions.workspace, revision: '3', inputJson: '-' }, {
        stdin: Readable.from([JSON.stringify({ file: '/tmp/sample.cbz', metadata_document: { fields: { title: secret } } })]),
        rpc: async () => { rejectedCalls++; },
    }), { code: 'invalid_input' });
    assert.equal(rejectedCalls, 0);
    const out = []; const err = []; let calls = 0;
    const code = await main(['--json', 'workspace', 'ai', 'extract', '--workspace', 'a'.repeat(32), '--revision', '0'], {
        stdout: { write: (value) => out.push(value) }, stderr: { write: (value) => err.push(value) },
        runWorkspaceCommand: async (action, subaction, options) => {
            calls++;
            const { runWorkspace } = await import('../workspace.mjs');
            return runWorkspace(action, subaction, options, { interactive: false, rpc: async () => { throw new Error('must not connect'); } });
        },
    });
    assert.equal(code, EXIT.interaction);
    assert.equal(calls, 1);
    assert.equal(out.length, 0);
    assert.equal(JSON.parse(err[0]).code, 'interaction_required');
});

test('task error review routes to the independent terminal command', async () => {
    const stdout = []; const stderr = [];
    let reviews = 0;
    const code = await main(['tasks', 'error-review', '--id', 'task-1'], {
        stdout: { write: (value) => stdout.push(value) }, stderr: { write: (value) => stderr.push(value) },
        runTasksCommand: async () => { throw new Error('generic task route must not run'); },
        runTaskErrorReviewCommand: async (options) => {
            reviews++;
            assert.equal(options.id, 'task-1');
            return { task_id: 'task-1', status: 'FAILED', has_error: true, details_reviewed: true, truncated: false };
        },
    });
    assert.equal(code, 0);
    assert.equal(reviews, 1);
    assert.deepEqual(stdout, ['错误详情已在独立终端显示。\n']);
    assert.deepEqual(stderr, []);
    const help = []; await main(['--help'], { stdout: { write: (value) => help.push(value) } });
    assert.match(help.join(''), /tasks error-review\s+--id ID/u);
});

test('JSONL attach emits only safe summaries and refuses review operations', async () => {
    const lines = [
        { op: 'text-set', expected_revision: 0, args: { text: secret } },
        { op: 'status' },
        { op: 'task-create-url', expected_revision: 1, args: { url: 'https://telegra.ph/example' } },
        { op: 'review' },
        { op: 'ai-review' },
    ];
    const output = []; let calls = 0;
    await runWorkspaceJSONL({ workspace: 'a'.repeat(32) }, {
        stdin: Readable.from(lines.map((value) => `${JSON.stringify(value)}\n`)),
        stdout: { write: (value) => { output.push(value); return true; } },
        rpc: async (_id, value) => {
            calls++;
            if (value.op === 'text-set') return { revision: 1, text_bytes: Buffer.byteLength(secret) };
            if (value.op === 'status') return { revision: 1, image_count: 0 };
            if (value.op === 'task-create-url') return { revision: 1, snapshot_attached: false, task: { id: 'task-1' } };
            throw new Error('forbidden operation reached broker');
        },
    });
    assert.equal(calls, 3);
    assert.equal(output.length, 5);
    assert.ok(!output.join('').includes(secret));
    assert.deepEqual(output.map((value) => JSON.parse(value).code), ['ok', 'ok', 'ok', 'invalid_input', 'invalid_input']);
    assert.deepEqual(output.map((value) => JSON.parse(value).seq), [0, 1, 2, 3, 4]);
});

test('clipboard is explicitly unavailable outside macOS', async () => {
    await assert.rejects(readClipboardImage({ platform: 'linux' }), { code: 'unsupported_platform' });
});

test('npm-style bin symlink executes help and version', async () => {
    const root = await mkdtemp('/tmp/mw-bin-');
    const link = join(root, 'mediactl');
    try {
        await symlink(fileURLToPath(new URL('../mediactl.mjs', import.meta.url)), link);
        const version = spawnSync(process.execPath, [link, '--version'], { encoding: 'utf8' });
        assert.equal(version.status, 0);
        assert.match(version.stdout, /^\d+\.\d+\.\d+\n$/u);
        const help = spawnSync(process.execPath, [link, '--help'], { encoding: 'utf8' });
        assert.equal(help.status, 0);
        assert.match(help.stdout, /workspace attach/u);
    } finally { await rm(root, { recursive: true, force: true }); }
});

test('private broker keeps draft across short-lived calls and rejects stale revision', async () => {
    const root = await mkdtemp('/tmp/mw-');
    const server = http.createServer((req, res) => {
        res.setHeader('Content-Type', 'application/json');
        if (req.url === '/api/auth/session') res.end(JSON.stringify({ authenticated: true, csrf_token: 'c'.repeat(43), expires_at: '2030-01-01T00:00:00Z' }));
        else if (req.url === '/api/metadata/schema') res.end(JSON.stringify(schema));
        else if (req.url === '/api/settings/extraction-rules') res.end(JSON.stringify(ruleSet));
        else { res.statusCode = 404; res.end('{}'); }
    });
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    const origin = `http://127.0.0.1:${address.port}`;
    const store = createSessionStore(join(root, 'config'));
    const directory = join(store.directory, 'workspaces');
    await store.save({ origin, cookie: 'a'.repeat(43), csrf: 'c'.repeat(43) });
    let id;
    try {
        const started = await startWorkspace({ server: origin, allowInsecureLoopback: true }, { directory, store });
        id = started.workspace_id;
        assert.equal(started.revision, 0);
        const changed = await workspaceRPC(id, { op: 'text-set', args: { text: `Title: ${secret}` }, expectedRevision: 0 }, { directory });
        assert.equal(changed.revision, 1);
        await assert.rejects(workspaceRPC(id, { op: 'text-set', args: { text: secret }, expectedRevision: 0 }, { directory }), { code: 'workspace_conflict' });
        const preview = await workspaceRPC(id, { op: 'rules-preview', expectedRevision: 1 }, { directory });
        assert.equal(preview.candidate_count, 1);
        assert.ok(!JSON.stringify(preview).includes(secret));
        const status = await workspaceRPC(id, { op: 'status' }, { directory });
        assert.equal(status.candidate_count, 1);
        assert.ok(!JSON.stringify(status).includes(secret));
        const review = await workspaceRPC(id, { op: 'review' }, { directory });
        assert.match(review.merged.text, /Private R18/);
    } finally {
        if (id) await workspaceRPC(id, { op: 'close', expectedRevision: (await workspaceRPC(id, { op: 'status' }, { directory })).revision }, { directory }).catch(() => {});
        await new Promise((resolve) => server.close(resolve));
        await rm(root, { recursive: true, force: true });
    }
});

test('private broker submits its document through URL and upload APIs without exposing values', async () => {
    const root = await mkdtemp('/tmp/mw-submit-');
    const archive = join(root, 'sample.cbz');
    await writeFile(archive, Buffer.from([0x50, 0x4b, 0x03, 0x04, 1, 2, 3]));
    const calls = [];
    let failUpload = true;
    const server = http.createServer(async (req, res) => {
        const chunks = []; for await (const chunk of req) chunks.push(chunk);
        const body = Buffer.concat(chunks);
        calls.push({ method: req.method, url: req.url, body, headers: req.headers });
        const send = (data, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)); };
        if (req.url === '/api/auth/session') return send({ authenticated: true, csrf_token: 'c'.repeat(43), expires_at: '2030-01-01T00:00:00Z' });
        if (req.url === '/api/client-contract') return send({ protocol_version: 1, client: 'mediactl' });
        if (req.url === '/api/metadata/schema') return send(schema);
        if (req.url === '/api/metadata/validate') return send({ document: JSON.parse(body).document, warnings: [] });
        if (req.url === '/download') {
            const submitted = JSON.parse(body);
            return send({ ok: true, duplicate: !submitted.force, needs_confirmation: !submitted.force,
                task: { id: 'task-url-1', task_type: 'url', status: 'READY' } });
        }
        if (req.url === '/api/tasks/upload/init') return send({ ok: true, task_id: 'task-upload-1', upload_url: '/api/tasks/task-upload-1/upload-source' }, 202);
        if (req.url === '/api/tasks/task-upload-1/upload-source') return failUpload ? send({ code: 'validation_error' }, 413) : send({ ok: true, task_id: 'task-upload-1' }, 202);
        if (req.url === '/api/tasks/task-upload-1') return send({ task: { id: 'task-upload-1', task_type: 'upload', status: 'READY' } });
        return send({}, 404);
    });
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const origin = `http://127.0.0.1:${server.address().port}`;
    const store = createSessionStore(join(root, 'config'));
    const directory = join(store.directory, 'workspaces');
    await store.save({ origin, cookie: 'a'.repeat(43), csrf: 'c'.repeat(43) });
    let id;
    try {
        const started = await startWorkspace({ server: origin, allowInsecureLoopback: true }, { directory, store });
        id = started.workspace_id;
        const field = await workspaceRPC(id, { op: 'field-set', args: { key: 'title', value: secret }, expectedRevision: 0 }, { directory });
        const duplicate = await workspaceRPC(id, { op: 'task-create-url', args: { url: 'https://telegra.ph/example' }, expectedRevision: field.revision }, { directory });
        assert.equal(duplicate.snapshot_attached, false);
        assert.equal(duplicate.revision, field.revision);
        const created = await workspaceRPC(id, { op: 'task-create-url', args: { url: 'https://telegra.ph/example', force: true }, expectedRevision: duplicate.revision }, { directory });
        assert.equal(created.snapshot_attached, true);
        assert.ok(!JSON.stringify({ duplicate, created }).includes(secret));
        const urlRequests = calls.filter((call) => call.url === '/download');
        assert.equal(urlRequests.length, 2);
        assert.equal(JSON.parse(urlRequests[1].body).metadata_document.fields.title.value, secret);
        assert.equal(urlRequests[1].headers['x-csrf-token'], 'c'.repeat(43));
        await assert.rejects(workspaceRPC(id, { op: 'task-upload', args: { file: archive }, expectedRevision: created.revision - 1 }, { directory }), { code: 'workspace_conflict' });
        await assert.rejects(workspaceRPC(id, { op: 'task-upload', args: { file: archive }, expectedRevision: created.revision }, { directory }), (error) => {
            assert.equal(error.code, 'upload_incomplete');
            assert.equal(error.taskId, 'task-upload-1');
            return true;
        });
        assert.equal((await workspaceRPC(id, { op: 'status' }, { directory })).revision, created.revision);
        failUpload = false;
        const uploaded = await workspaceRPC(id, { op: 'task-upload', args: { file: archive }, expectedRevision: created.revision }, { directory });
        assert.equal(uploaded.upload_complete, true);
        assert.equal(uploaded.snapshot_attached, true);
        assert.ok(!JSON.stringify(uploaded).includes(secret));
        assert.equal(JSON.parse(calls.find((call) => call.url === '/api/tasks/upload/init').body).metadata_document.fields.title.value, secret);
        assert.equal((await workspaceRPC(id, { op: 'review' }, { directory })).document.fields.title.value, secret);
    } finally {
        if (id) await workspaceRPC(id, { op: 'close', expectedRevision: (await workspaceRPC(id, { op: 'status' }, { directory })).revision }, { directory }).catch(() => {});
        await new Promise((resolve) => server.close(resolve));
        await rm(root, { recursive: true, force: true });
    }
});
