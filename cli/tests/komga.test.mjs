import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { Readable } from 'node:stream';
import test from 'node:test';
import { runKomga, runKomgaConnection } from '../komga.mjs';
import { main, parseArgs } from '../mediactl.mjs';

const cookie = 'a'.repeat(43);
const csrf = 'b'.repeat(43);
const bookID = 'book_123';
const operationID = 'a'.repeat(32);
const sourceVersion = 'b'.repeat(64);
const previewToken = `1234567890.${'c'.repeat(64)}.${'d'.repeat(64)}`;
const privateText = 'PRIVATE_R18_METADATA';

function respond(res, data, status = 200) {
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(data));
}

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

const options = (server, more = {}) => ({ store: server.store, allowInsecureLoopback: true, ...more });
const input = (data) => ({ inputJson: '-', stdin: Readable.from([JSON.stringify(data)]) });
const book = { id: bookID, library_id: 'library_1', title: privateText, file_name: `${privateText}.cbz`, file_type: '.cbz', editable: true,
    metadata: { summary: privateText } };
const detail = { book, source_version: sourceVersion, document: { definitions_version: 'v1', fields: { summary: { state: 'value', value: privateText } } },
    fields: [{ key: 'summary', type: 'string', label: privateText, can_set: true, can_clear: true }],
    has_comicinfo: true, page_count: 1, warnings: [{ key: 'summary', code: 'legacy_field', message: privateText }], can_save: true };
const changes = { source_version: sourceVersion, definitions_version: 'v1', changes: [{ key: 'summary', state: 'value', value: privateText }] };

test('Komga command entrypoint parses and routes three-level commands', async () => {
    assert.equal(parseArgs(['komga', 'books', 'list', '--library-id', 'library_1', '--size', '5']).libraryId, 'library_1');
    const output = [];
    const code = await main(['--json', 'komga', 'libraries', 'list'], {
        stdout: { write: (value) => output.push(value) }, stderr: { write: () => {} },
        runKomgaCommand: async (section, action) => ({ section, action }),
    });
    assert.equal(code, 0);
    assert.deepEqual(JSON.parse(output[0]).data, { section: 'libraries', action: 'list' });
});

test('successful logout closes in-memory workspaces without failing on cleanup errors', async () => {
    const output = [];
    let closed = 0;
    const result = await main(['--json', 'auth', 'logout'], {
        stdout: { write: (value) => output.push(value) }, stderr: { write: () => {} },
        runAuthCommand: async () => ({ authenticated: false }),
        closeAllWorkspacesCommand: async () => { closed += 1; throw new Error('stale socket'); },
    });
    assert.equal(result, 0);
    assert.equal(closed, 1);
    assert.equal(JSON.parse(output[0]).data.authenticated, false);
});

test('Komga connection uses CAS and hidden TTY credential without returning target or key', async (t) => {
    const server = await fixture((req, res, body) => {
        if (req.url === '/api/settings/komga' && req.method === 'GET') return respond(res, { base_url: `https://${privateText}.example`, credential_configured: true, config_version: 3 });
        if (req.url === '/api/settings/komga' && req.method === 'PUT') {
            assert.deepEqual(JSON.parse(body), { expected_version: 3, base_url: 'https://komga.example', credential: { action: 'replace', value: privateText } });
            return respond(res, { base_url: 'https://komga.example', credential_configured: true, config_version: 4 });
        }
        if (req.url === '/api/settings/komga/test') {
            assert.deepEqual(JSON.parse(body), { config_version: 4 });
            return respond(res, { status: 'connected', allowed_library_count: 2, upstream: privateText });
        }
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const status = await runKomgaConnection('status', options(server));
    const toSet = input({ expected_version: 3, base_url: 'https://komga.example' });
    const saved = await runKomgaConnection('set', options(server, { credential: 'replace', inputJson: '-', interactive: true }),
        { stdin: toSet.stdin, secretReader: async () => privateText, interactive: true });
    const tested = await runKomgaConnection('test', options(server, { configVersion: '4' }));
    assert.deepEqual(status, { target_configured: true, credential_configured: true, config_version: 3 });
    assert.equal(saved.config_version, 4);
    assert.deepEqual(tested, { status: 'connected', allowed_library_count: 2 });
    assert.doesNotMatch(JSON.stringify({ status, saved, tested }), new RegExp(privateText));
    const writes = server.calls.filter((call) => ['PUT', 'POST'].includes(call.method));
    assert.ok(writes.every((call) => call.headers.origin === server.origin && call.headers['x-csrf-token'] === csrf));
});

test('noninteractive Komga key replacement and review stop before authentication', async () => {
    const store = { load: async () => { throw new Error('session must not be read'); } };
    await assert.rejects(runKomgaConnection('set', { store, credential: 'replace', json: true }), { code: 'interaction_required' });
    await assert.rejects(runKomgaConnection('set', { store, credential: 'replace' }, { interactive: false }), { code: 'interaction_required' });
    await assert.rejects(runKomga('books', 'review', { store, id: bookID, json: true }), { code: 'interaction_required' });
    await assert.rejects(runKomga('metadata', 'review', { store, id: bookID }, { interactive: false }), { code: 'interaction_required' });
});

test('Komga catalog and edit detail expose IDs and capabilities without adult metadata', async (t) => {
    const server = await fixture((req, res) => {
        if (req.url === '/api/komga/libraries') return respond(res, { libraries: [{ id: 'library_1', name: privateText, writable: true, unavailable: false }] });
        if (req.url?.startsWith('/api/komga/books?')) return respond(res, { books: [book], page: 0, size: 25, total_pages: 1, total_elements: 1 });
        if (req.url === `/api/komga/books/${bookID}`) return respond(res, { book });
        if (req.url === `/api/komga/books/${bookID}/edit`) return respond(res, detail);
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const libraries = await runKomga('libraries', 'list', options(server));
    const books = await runKomga('books', 'list', options(server, { libraryId: 'library_1', query: privateText }));
    const shown = await runKomga('books', 'show', options(server, { id: bookID }));
    const edited = await runKomga('books', 'edit', options(server, { id: bookID }));
    let reviewed;
    const review = await runKomga('books', 'review', options(server, { id: bookID }), { interactive: true, reviewer: async (text) => { reviewed = text; } });
    assert.equal(libraries.libraries[0].id, 'library_1');
    assert.equal(books.books[0].id, bookID);
    assert.equal(books.books[0].file_type, '.cbz');
    assert.equal(shown.book.editable, true);
    assert.equal(shown.book.file_type, '.cbz');
    assert.equal(edited.source_version, sourceVersion);
    assert.deepEqual(edited.present_keys, ['summary']);
    assert.equal(review.reviewed, true);
    assert.match(reviewed, new RegExp(privateText));
    assert.doesNotMatch(JSON.stringify({ libraries, books, shown, edited, review }), new RegExp(privateText));
    assert.ok(server.calls.some((call) => call.path.includes(`query=${privateText}`)));
});

test('Komga preview strips values; update sends same preview and idempotency key then reads operation', async (t) => {
    const writes = [];
    const server = await fixture((req, res, body) => {
        if (req.url === `/api/komga/books/${bookID}/preview`) {
            writes.push(JSON.parse(body));
            return respond(res, { preview_token: previewToken, expires_at: '2030-01-01T00:00:00Z', file_changed: true, can_save: true,
                diffs: [{ key: 'summary', action: 'value', before: privateText, after: privateText }],
                warnings: [{ key: 'summary', code: 'changed', message: privateText }] });
        }
        if (req.url === `/api/komga/books/${bookID}/save`) {
            writes.push(JSON.parse(body));
            return respond(res, { operation: operation('file_committed') });
        }
        if (req.url === `/api/komga/edits/${operationID}`) return respond(res, { operation: operation('sync_pending') });
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const previewInput = input(changes);
    const preview = await runKomga('metadata', 'preview', options(server, { id: bookID, inputJson: '-' }), { stdin: previewInput.stdin });
    let reviewed;
    const reviewInput = input(changes);
    await runKomga('metadata', 'review', options(server, { id: bookID, inputJson: '-' }),
        { stdin: reviewInput.stdin, interactive: true, reviewer: async (text) => { reviewed = text; } });
    const saveInput = input({ ...changes, preview_token: preview.preview_token, idempotency_key: 'same_save_request_1234' });
    const result = await runKomga('metadata', 'update', options(server, { id: bookID, inputJson: '-' }), { stdin: saveInput.stdin });
    assert.equal(preview.diffs[0].key, 'summary');
    assert.match(reviewed, new RegExp(privateText));
    assert.equal(result.operation.state, 'sync_pending');
    assert.equal(result.verification_pending, false);
    assert.equal(writes.length, 3);
    assert.deepEqual(writes[0], changes);
    assert.deepEqual(writes[2], { ...changes, preview_token: preview.preview_token, idempotency_key: 'same_save_request_1234' });
    assert.doesNotMatch(JSON.stringify({ preview, result }), new RegExp(privateText));
});

test('explicit PageCount correction survives preview and save with no other field changes', async (t) => {
    const posted = [];
    const server = await fixture((req, res, body) => {
        if (req.url === `/api/komga/books/${bookID}/edit`) return respond(res, { ...detail, page_count_correction_needed: true });
        if (req.url === `/api/komga/books/${bookID}/preview`) {
            posted.push(JSON.parse(body));
            return respond(res, { preview_token: previewToken, expires_at: '2030-01-01T00:00:00Z', file_changed: true, can_save: true,
                diffs: [{ key: 'page_count', action: 'corrected', before: 999, after: 1 }] });
        }
        if (req.url === `/api/komga/books/${bookID}/save`) {
            posted.push(JSON.parse(body));
            return respond(res, { operation: operation('file_committed') });
        }
        if (req.url === `/api/komga/edits/${operationID}`) return respond(res, { operation: operation('sync_pending') });
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const edit = await runKomga('books', 'edit', options(server, { id: bookID }));
    assert.equal(edit.page_count_correction_needed, true);
    const correction = { source_version: sourceVersion, definitions_version: 'v1', changes: [], correct_page_count: true };
    const previewInput = input(correction);
    const preview = await runKomga('metadata', 'preview', options(server, { id: bookID, inputJson: '-' }), { stdin: previewInput.stdin });
    assert.equal(preview.page_count_correction, true);
    assert.deepEqual(preview.diffs, [{ key: 'page_count', action: 'corrected' }]);
    const save = { ...correction, preview_token: preview.preview_token, idempotency_key: 'correct_page_count_1234' };
    const saveInput = input(save);
    await runKomga('metadata', 'update', options(server, { id: bookID, inputJson: '-' }), { stdin: saveInput.stdin });
    assert.deepEqual(posted, [correction, save]);
    const invalidInput = input({ ...correction, correct_page_count: 'true' });
    await assert.rejects(runKomga('metadata', 'preview', options(server, { id: bookID, inputJson: '-' }), { stdin: invalidInput.stdin }), { code: 'invalid_input' });
    assert.equal(posted.length, 2);
});

function operation(state, actions = []) {
    return { id: operationID, book_id: bookID, state, file_committed: true, file_no_change: false,
        file_restored: false, projection_applicable: true, projection_consistent: false, analyze_verified: false,
        last_error_code: 'retry_required', available_actions: actions, private: privateText };
}

test('Komga sync and restore check eligibility, post empty body, and read back state', async (t) => {
    let state = 'sync_failed';
    const server = await fixture((req, res, body) => {
        if (req.url === `/api/komga/edits/${operationID}` && req.method === 'GET') {
            return respond(res, { operation: operation(state, state === 'sync_failed' ? ['retry_sync'] : state === 'restore_needed' ? ['restore'] : []) });
        }
        if (req.url === `/api/komga/edits/${operationID}/sync`) {
            assert.deepEqual(JSON.parse(body), {});
            state = 'current_value_consistent';
            return respond(res, { operation: operation(state) });
        }
        if (req.url === `/api/komga/edits/${operationID}/restore`) {
            assert.deepEqual(JSON.parse(body), {});
            state = 'restored';
            return respond(res, { operation: operation(state) });
        }
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const synced = await runKomga('edits', 'sync', options(server, { id: operationID }));
    assert.equal(synced.operation.state, 'current_value_consistent');
    await assert.rejects(runKomga('edits', 'sync', options(server, { id: operationID })), { code: 'edit_conflict' });
    state = 'restore_needed';
    const restored = await runKomga('edits', 'restore', options(server, { id: operationID }));
    assert.equal(restored.operation.state, 'restored');
    assert.doesNotMatch(JSON.stringify({ synced, restored }), new RegExp(privateText));
    const writes = server.calls.filter((call) => call.method === 'POST');
    assert.equal(writes.length, 2);
    assert.ok(writes.every((call) => call.headers.origin === server.origin && call.headers['x-csrf-token'] === csrf));
});

test('Komga preview and connection errors do not expose upstream text', async (t) => {
    const server = await fixture((req, res) => {
        if (req.url === '/api/settings/komga') return respond(res, { code: 'config_conflict', message: privateText }, 409);
        if (req.url === `/api/komga/books/${bookID}/preview`) return respond(res, { code: 'edit_conflict', message: privateText }, 409);
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    await assert.rejects(runKomgaConnection('status', options(server)), (error) => error.code === 'config_conflict' && !error.message.includes(privateText));
    const toPreview = input(changes);
    await assert.rejects(runKomga('metadata', 'preview', options(server, { id: bookID, inputJson: '-' }), { stdin: toPreview.stdin }),
        (error) => error.code === 'edit_conflict' && error.exitCode === 4 && !error.message.includes(privateText));
});
