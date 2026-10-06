import test from 'node:test';
import assert from 'node:assert/strict';

import { cancelUnfinishedUpload, submitArchive, uploadArchiveSource } from '../home/upload_submission.js';

test('submitArchive initializes task then streams file with xhr progress', async () => {
    const progressSnapshots = [];
    const api = {
        async postJson(url, payload) {
            assert.equal(url, '/api/tasks/upload/init');
            assert.equal(payload.file_name, 'demo.zip');
            assert.equal(payload.file_size, 40);
            return {
                response: { ok: true, status: 202 },
                payload: {
                    ok: true,
                    task_id: 'task-upload-1',
                    upload_token: 'upload-token-1',
                    upload_url: '/api/tasks/task-upload-1/upload-source',
                    logs_url: '/logs',
                },
            };
        },
    };

    const xhrCalls = [];
    const file = { name: 'demo.zip', size: 40 };

    const result = await submitArchive({
        api,
        file,
        metadata: { author: '作者A' },
        onProgress(snapshot) {
            progressSnapshots.push(snapshot);
        },
        createXHR() {
            const handlers = {};
            const uploadHandlers = {};
            const xhr = {
                upload: {
                    addEventListener(eventName, handler) {
                        uploadHandlers[eventName] = handler;
                    },
                },
                addEventListener(eventName, handler) {
                    handlers[eventName] = handler;
                },
                open(method, url) {
                    xhrCalls.push({ method, url });
                },
                setRequestHeader(name, value) {
                    xhrCalls.push({ header: name, value });
                },
                send() {
                    uploadHandlers.progress({ lengthComputable: true, loaded: 12, total: 40 });
                    this.status = 202;
                    this.responseText = JSON.stringify({ ok: true, task_id: 'task-upload-1', status: 'QUEUED' });
                    handlers.load();
                },
            };
            return xhr;
        },
    });

    assert.equal(result.initPayload.task_id, 'task-upload-1');
    assert.equal(result.uploadPayload.status, 'QUEUED');
    assert.deepEqual(progressSnapshots[0], {
        status: 'uploading',
        loadedBytes: 12,
        totalBytes: 40,
        fileName: 'demo.zip',
        errorMessage: '',
        canCancel: true,
    });
    assert.equal(xhrCalls[0].method, 'PUT');
    assert.equal(xhrCalls[0].url, '/api/tasks/task-upload-1/upload-source');
});


function createPendingUpload(options = {}) {
    const handlers = {};
    let sends = 0;
    const xhr = {
        status: 202,
        responseText: '{"ok":true}',
        upload: { addEventListener() {} },
        addEventListener(name, handler) { handlers[name] = handler; },
        open() {},
        setRequestHeader() {},
        send() { sends += 1; },
        abort() { handlers.abort(); },
    };
    const promise = uploadArchiveSource({
        uploadUrl: '/api/tasks/upload-1/upload-source',
        file: { name: 'demo.zip', size: 40 },
        createXHR: () => xhr,
        ...options,
    });
    return { handlers, xhr, promise, sends: () => sends };
}

test('HTML upload errors reject and leave the submit flow able to finish', async () => {
    const { handlers, xhr, promise } = createPendingUpload();
    xhr.status = 413;
    xhr.responseText = '<html>413 Request Entity Too Large</html>';
    assert.doesNotThrow(() => handlers.load());
    await assert.rejects(promise, /413/);
});

test('upload rejects malformed successful responses and business failures', async () => {
    for (const responseText of ['', '<html>Bad gateway</html>', '{"ok":false,"message":"上传被拒绝。"}']) {
        const { handlers, xhr, promise } = createPendingUpload();
        xhr.responseText = responseText;
        assert.doesNotThrow(() => handlers.load());
        await assert.rejects(promise, /上传/);
    }
});

test('upload abort and timeout events reject rather than hanging', async () => {
    for (const event of ['abort', 'timeout']) {
        const { handlers, xhr, promise } = createPendingUpload();
        assert.equal(typeof handlers[event], 'function');
        assert.ok(xhr.timeout > 0);
        handlers[event]();
        await assert.rejects(promise, event === 'abort' ? /取消/ : /超时/);
    }
});

test('page abort stops an in-flight XHR and does not start an already aborted upload', { timeout: 1000 }, async () => {
    const controller = new AbortController();
    const { promise, sends } = createPendingUpload({ signal: controller.signal });
    assert.equal(sends(), 1);
    controller.abort();
    await assert.rejects(promise, { name: 'AbortError' });
    const aborted = createPendingUpload({ signal: controller.signal });
    await assert.rejects(aborted.promise, { name: 'AbortError' });
    assert.equal(aborted.sends(), 0);
});

test('lost init response recovers the same key and skips streaming an already accepted archive', async () => {
    const calls = []; let xhrs = 0;
    const result = await submitArchive({
        api: {
            async postJson(url, payload) { calls.push([url, payload.idempotency_key, payload.file_sha256]); throw new Error('connection lost'); },
            async getJson(url) { calls.push([url]); return { response: { ok: true }, payload: { ok: true, task_id: 'same-task', status: 'READY' } }; },
        },
        file: { name: 'sample.zip', size: 42 }, metadata: { idempotency_key: 'stable-submission-key', file_sha256: 'a'.repeat(64) },
        createXHR() { xhrs += 1; },
    });
    assert.equal(result.initPayload.task_id, 'same-task'); assert.equal(xhrs, 0);
    assert.deepEqual(calls, [['/api/tasks/upload/init', 'stable-submission-key', 'a'.repeat(64)], ['/api/tasks/submissions/stable-submission-key']]);
});

test('lost upload response is recovered without reuploading or canceling an accepted task', async () => {
    const api = {
        async postJson() { return { response: { ok: true }, payload: { ok: true, task_id: 'same-task', status: 'CREATED', upload_url: '/api/tasks/same-task/upload-source' } }; },
        async getJson() { return { response: { ok: true }, payload: { ok: true, task_id: 'same-task', status: 'RUNNING' } }; },
    };
    const result = await submitArchive({ api, file: { name: 'sample.zip', size: 42 }, metadata: { idempotency_key: 'stable-submission-key' }, createXHR() {
        const handlers = {}; return { upload: { addEventListener() {} }, addEventListener(event, handler) { handlers[event] = handler; }, open() {}, setRequestHeader() {}, send() { handlers.error(); } };
    } });
    assert.equal(result.uploadPayload.status, 'RUNNING');
});

test('a canceled page does not issue a submission recovery request or start an upload', async () => {
    const controller = new AbortController(); let reads = 0; let uploads = 0;
    await assert.rejects(submitArchive({
        api: { async postJson() { controller.abort(); throw new DOMException('cancelled', 'AbortError'); }, async getJson() { reads += 1; } },
        signal: controller.signal, file: { name: 'sample.zip', size: 42 }, metadata: { idempotency_key: 'stable-submission-key' }, createXHR() { uploads += 1; },
    }), { name: 'AbortError' });
    assert.equal(reads, 0); assert.equal(uploads, 0);
});

test('leaving cancels only a still-CREATED upload, with no inherited aborted page signal', async () => {
    const canceled = [];
    const api = { async getJson(url, options) { assert.equal(options, undefined); return { response: { ok: true }, payload: { status: url.endsWith('unfinished') ? 'CREATED' : 'READY' } }; }, async postJson(url, body, options) { assert.equal(options, undefined); canceled.push(url); } };
    await cancelUnfinishedUpload(api, 'accepted'); await cancelUnfinishedUpload(api, 'unfinished');
    assert.deepEqual(canceled, ['/api/tasks/unfinished/cancel']);
});
