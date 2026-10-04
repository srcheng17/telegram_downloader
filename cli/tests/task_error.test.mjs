import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import test from 'node:test';
import { runTaskErrorReview } from '../task_error.mjs';

const taskID = 'task_123';
const cookie = 'a'.repeat(43);
const csrf = 'b'.repeat(43);
const privateText = 'PRIVATE_R18_ERROR_CONTENT';

test('task error review refuses JSON and absent TTY before reading session', async () => {
    const store = { load: async () => { throw new Error('session must not be read'); } };
    await assert.rejects(runTaskErrorReview({ id: taskID, store, json: true }), { code: 'interaction_required' });
    await assert.rejects(runTaskErrorReview({ id: taskID, store }, { interactive: false }), { code: 'interaction_required' });
});

test('task error review bounds private text to TTY and returns only a safe summary', async (t) => {
    const calls = [];
    const app = createServer((req, res) => {
        calls.push(req.url);
        res.writeHead(200, { 'Content-Type': 'application/json' });
        if (req.url === '/api/auth/session') return res.end(JSON.stringify({ authenticated: true, csrf_token: csrf, expires_at: 'future' }));
        if (req.url === `/api/tasks/${taskID}`) return res.end(JSON.stringify({ task: {
            id: taskID, task_type: 'url', status: 'FAILED', error: privateText.repeat(1000),
        } }));
        return res.end('{}');
    });
    app.listen(0, '127.0.0.1');
    await once(app, 'listening');
    t.after(() => app.close());
    const origin = `http://127.0.0.1:${app.address().port}`;
    const store = { load: async () => ({ origin, cookie, csrf }), save: async () => {} };
    let terminal;
    const result = await runTaskErrorReview({ id: taskID, store, allowInsecureLoopback: true }, {
        interactive: true, reviewer: async (value) => { terminal = value; },
    });
    assert.equal(result.task_id, taskID);
    assert.equal(result.has_error, true);
    assert.equal(result.truncated, true);
    assert.equal(result.details_reviewed, true);
    assert.ok(terminal.error_text.includes(privateText));
    assert.ok(Buffer.byteLength(terminal.error_text) <= 8192);
    assert.doesNotMatch(JSON.stringify(result), new RegExp(privateText));
    assert.deepEqual(calls, ['/api/auth/session', `/api/tasks/${taskID}`]);
});
