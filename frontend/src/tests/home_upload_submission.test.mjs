import test from 'node:test';
import assert from 'node:assert/strict';

import { submitArchive } from '../home/upload_submission.js';

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
