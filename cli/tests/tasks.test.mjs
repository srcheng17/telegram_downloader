import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Readable } from 'node:stream';
import test from 'node:test';
import { runOverview, runTasks } from '../tasks.mjs';
import { errorEnvelope } from '../errors.mjs';

const cookie = 'a'.repeat(43);
const csrf = 'b'.repeat(43);
const privateText = 'PRIVATE_METADATA_AND_ERROR';
const zip = Buffer.from([0x50, 0x4b, 0x03, 0x04, 0x01, 0x02, 0x03]);

function taskView(status = 'SUCCEEDED', availableActions = ['download', 'copy_to_komga']) {
    return {
        id: 'task-1', task_type: 'url', status, available_actions: availableActions,
        progress: { phase: 'completed', current: 2, total: 2, unit: 'images', message: privateText },
        url: `https://telegra.ph/${privateText}`, artifact_name: `${privateText}.cbz`,
        error: privateText, summary: privateText,
        metadata_document: { fields: { summary: { value: privateText } } },
    };
}

async function mockServer(handler) {
    const calls = [];
    const app = createServer(async (req, res) => {
        let body = Buffer.alloc(0);
        for await (const chunk of req) body = Buffer.concat([body, chunk]);
        calls.push({ method: req.method, url: req.url, headers: req.headers, body });
        if (req.url === '/api/auth/session') {
            res.setHeader('Content-Type', 'application/json');
            res.end(JSON.stringify({ authenticated: true, csrf_token: csrf, expires_at: 'future' }));
            return;
        }
        if (req.url === '/api/client-contract') {
            res.setHeader('Content-Type', 'application/json');
            res.end(JSON.stringify({ protocol_version: 1, client: 'mediactl' }));
            return;
        }
        await handler(req, res, body);
    });
    app.listen(0, '127.0.0.1');
    await once(app, 'listening');
    const origin = `http://127.0.0.1:${app.address().port}`;
    const store = {
        load: async () => ({ origin, cookie, csrf, expiresAt: 'future' }),
        save: async () => {}, clear: async () => {},
    };
    return { app, calls, origin, store };
}

const options = (fixture, more = {}) => ({ store: fixture.store, allowInsecureLoopback: true, ...more });
const json = (res, data, status = 200) => {
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(data));
};

test('overview, filtered list and show expose only safe projections', async (t) => {
    const fixture = await mockServer((req, res) => {
        if (req.url === '/api/summary') return json(res, { total_tasks: 7, active_tasks: 2, startup_recovery: { happened: true, recovered_total: 1 }, secret: privateText });
        if (req.url.startsWith('/api/logs?')) return json(res, { logs: [taskView()], total: 1, page: 2, per_page: 10, total_pages: 2, has_active_tasks: false, secret: privateText });
        if (req.url === '/api/tasks/task-1') return json(res, { ok: true, task: taskView() });
        return json(res, {}, 404);
    });
    t.after(() => fixture.app.close());
    const overview = await runOverview(options(fixture));
    const list = await runTasks('list', options(fixture, { status: 'SUCCEEDED', query: 'keyword', page: '2', perPage: '10' }));
    const show = await runTasks('show', options(fixture, { id: 'task-1' }));
    assert.equal(overview.total_tasks, 7);
    assert.equal(overview.startup_recovery.recovered_total, 1);
    assert.equal(list.tasks[0].has_error, true);
    assert.equal(show.task.status, 'SUCCEEDED');
    assert.equal(fixture.calls.find((call) => call.url.startsWith('/api/logs?')).url, '/api/logs?page=2&per_page=10&status=SUCCEEDED&q=keyword');
    assert.doesNotMatch(JSON.stringify({ overview, list, show }), new RegExp(privateText));
});

test('cancel reports pending state after readback; retry obeys action eligibility', async (t) => {
    let status = 'RUNNING';
    const fixture = await mockServer((req, res) => {
        if (req.url === '/api/tasks/task-1' && req.method === 'GET') {
            return json(res, { task: taskView(status, status === 'RUNNING' ? ['cancel'] : status === 'CANCELED' ? ['retry'] : []) });
        }
        if (req.url === '/api/tasks/task-1/cancel') { status = 'CANCELING'; return json(res, { ok: true, task: taskView(status, []) }); }
        if (req.url === '/api/tasks/task-1/retry') { status = 'READY'; return json(res, { ok: true, task: taskView(status, []) }); }
        return json(res, {}, 404);
    });
    t.after(() => fixture.app.close());
    const cancelled = await runTasks('cancel', options(fixture, { id: 'task-1' }));
    assert.deepEqual({ requested: cancelled.requested, final: cancelled.final, cancelled: cancelled.cancelled }, { requested: true, final: false, cancelled: false });
    assert.equal(fixture.calls.find((call) => call.url.endsWith('/cancel')).headers.origin, fixture.origin);
    assert.equal(fixture.calls.find((call) => call.url.endsWith('/cancel')).headers['x-csrf-token'], csrf);
    await assert.rejects(runTasks('retry', options(fixture, { id: 'task-1' })), { code: 'action_unavailable' });
    status = 'CANCELED';
    const retried = await runTasks('retry', options(fixture, { id: 'task-1' }));
    assert.equal(retried.task.status, 'READY');
    assert.equal(retried.final, false);
});

test('wait polls to terminal state and timeout reports task ID safely', async (t) => {
    let status = 'RUNNING';
    let reads = 0;
    const fixture = await mockServer((req, res) => {
        if (req.url === '/api/tasks/task-1') { reads += 1; return json(res, { task: taskView(status, []) }); }
        return json(res, {}, 404);
    });
    t.after(() => fixture.app.close());
    const waited = await runTasks('wait', options(fixture, { id: 'task-1', timeout: '3', interval: '1' }), { sleep: async () => { status = 'SUCCEEDED'; } });
    assert.equal(waited.final, true);
    assert.equal(reads, 2);
    let time = 0;
    status = 'RUNNING';
    await assert.rejects(runTasks('wait', options(fixture, { id: 'task-1', timeout: '1', interval: '1' }), {
        now: () => time, sleep: async () => { time = 1000; status = 'RUNNING'; },
    }), (error) => {
        assert.deepEqual(errorEnvelope(error).data, { task_id: 'task-1' });
        return error.code === 'wait_timeout';
    });
});

test('URL creation returns duplicate confirmation without claiming new task and forwards raw JSON', async (t) => {
    const fixture = await mockServer((req, res) => {
        if (req.url === '/download') return json(res, { ok: true, duplicate: true, needs_confirmation: true, task: taskView() });
        return json(res, {}, 404);
    });
    t.after(() => fixture.app.close());
    const result = await runTasks('create-url', options(fixture, { url: 'https://telegra.ph/example' }));
    assert.equal(result.created, false);
    assert.equal(result.needs_confirmation, true);
    const raw = '{"url":"https://telegra.ph/example","force":true,"metadata_document":"{\\"fields\\":{}}"}';
    await runTasks('create-url', options(fixture, { inputJson: '-', force: true }), { stdin: Readable.from([raw]) });
    assert.equal(fixture.calls.filter((call) => call.url === '/download')[1].body.toString(), raw);
    await assert.rejects(runTasks('create-url', options(fixture, { inputJson: '-' }), { stdin: Readable.from([raw]) }), { code: 'invalid_input' });
});

test('upload uses init then protected PUT then readback, and partial failure carries task ID', async (t) => {
    const parent = await mkdtemp(join(tmpdir(), 'mediactl-upload-'));
    t.after(() => rm(parent, { recursive: true, force: true }));
    const file = join(parent, 'sample.cbz');
    await writeFile(file, zip);
    let failPUT = false;
    const fixture = await mockServer((req, res, body) => {
        if (req.url === '/api/tasks/upload/init') {
            const input = JSON.parse(body);
            assert.deepEqual({ name: input.file_name, size: input.file_size }, { name: 'sample.cbz', size: zip.length });
            return json(res, { ok: true, task_id: 'task-1', upload_url: '/api/tasks/task-1/upload-source?file_name=sample.cbz' }, 202);
        }
        if (req.url.startsWith('/api/tasks/task-1/upload-source')) {
            assert.equal(req.headers['content-length'], String(zip.length));
            assert.equal(req.headers['x-csrf-token'], csrf);
            assert.deepEqual(body, zip);
            return failPUT ? json(res, { code: 'validation_error' }, 413) : json(res, { ok: true, task_id: 'task-1' }, 202);
        }
        if (req.url === '/api/tasks/task-1') return json(res, { task: taskView('READY', ['cancel']) });
        return json(res, {}, 404);
    });
    t.after(() => fixture.app.close());
    const uploaded = await runTasks('upload', options(fixture, { file }));
    assert.equal(uploaded.upload_complete, true);
    assert.equal(uploaded.task.status, 'READY');
    failPUT = true;
    await assert.rejects(runTasks('upload', options(fixture, { file })), (error) => {
        assert.equal(error.code, 'upload_incomplete');
        assert.deepEqual(errorEnvelope(error).data, { task_id: 'task-1' });
        return true;
    });
});

test('artifact check/download writes atomically without overwrite or private path output', async (t) => {
    const parent = await mkdtemp(join(tmpdir(), 'mediactl-artifact-'));
    t.after(() => rm(parent, { recursive: true, force: true }));
    const output = join(parent, 'result.cbz');
    const fixture = await mockServer((req, res) => {
        if (req.url === '/api/tasks/task-1') return json(res, { task: taskView() });
        if (req.url === '/api/tasks/task-1/download' && req.method === 'HEAD') { res.writeHead(204); res.end(); return; }
        if (req.url === '/api/tasks/task-1/download') {
            res.writeHead(200, { 'Content-Type': 'application/vnd.comicbook+zip', 'Content-Length': zip.length });
            res.end(zip);
            return;
        }
        return json(res, {}, 404);
    });
    t.after(() => fixture.app.close());
    assert.deepEqual(await runTasks('artifact-check', options(fixture, { id: 'task-1' })), { task_id: 'task-1', available: true });
    const result = await runTasks('artifact-get', options(fixture, { id: 'task-1', output }));
    assert.equal(result.bytes, zip.length);
    assert.equal(result.saved, true);
    assert.equal((await stat(output)).mode & 0o777, 0o600);
    assert.deepEqual(await readFile(output), zip);
    assert.doesNotMatch(JSON.stringify(result), /mediactl-artifact-|PRIVATE_METADATA_AND_ERROR/);
    await assert.rejects(runTasks('artifact-get', options(fixture, { id: 'task-1', output })), { code: 'target_exists' });
    assert.deepEqual(await readFile(output), zip);
});

test('copy response target_path is never exposed, and Komga indexing remains unverified', async (t) => {
    const fixture = await mockServer((req, res) => {
        if (req.url === '/api/tasks/task-1') return json(res, { task: taskView() });
        if (req.url === '/api/tasks/task-1/copy-to-komga') return json(res, { ok: true, task_id: 'task-1', target_path: `/private/${privateText}` });
        return json(res, {}, 404);
    });
    t.after(() => fixture.app.close());
    const result = await runTasks('copy-to-komga', options(fixture, { id: 'task-1' }));
    assert.equal(result.copy_completed, true);
    assert.equal(result.komga_indexed, 'unverified');
    assert.doesNotMatch(JSON.stringify(result), /private|PRIVATE_METADATA_AND_ERROR/);
});

test('artifact rejects HTML or invalid ZIP without publishing output', async (t) => {
    const parent = await mkdtemp(join(tmpdir(), 'mediactl-artifact-bad-'));
    t.after(() => rm(parent, { recursive: true, force: true }));
    let badType = true;
    const fixture = await mockServer((req, res) => {
        if (req.url === '/api/tasks/task-1') return json(res, { task: taskView() });
        if (req.url === '/api/tasks/task-1/download' && req.method === 'HEAD') { res.writeHead(204); res.end(); return; }
        if (req.url === '/api/tasks/task-1/download') {
            const body = badType ? Buffer.from('<html>secret</html>') : Buffer.from('not a zip');
            res.writeHead(200, { 'Content-Type': badType ? 'text/html' : 'application/zip', 'Content-Length': body.length });
            res.end(body);
            return;
        }
        return json(res, {}, 404);
    });
    t.after(() => fixture.app.close());
    const output = join(parent, 'bad.cbz');
    await assert.rejects(runTasks('artifact-get', options(fixture, { id: 'task-1', output })), { code: 'invalid_artifact' });
    badType = false;
    await assert.rejects(runTasks('artifact-get', options(fixture, { id: 'task-1', output })), { code: 'invalid_artifact' });
    await assert.rejects(stat(output), { code: 'ENOENT' });
});
