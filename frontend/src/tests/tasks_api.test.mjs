import test from 'node:test';
import assert from 'node:assert/strict';
import { createTasksApi } from '../shared/api/tasks_api.js';

test('business transport adds session CSRF to mutations and never sends it to another origin', async () => {
    let token = '';
    const calls = [];
    const win = { location: { origin: 'https://workspace.test' }, __adminSession: {
        getCSRFToken: () => token, isAuthenticated: () => true,
        async refresh() { token = 'csrf-from-session'; },
    } };
    const api = createTasksApi(async (url, options) => { calls.push({ url, options }); return new Response('{}'); }, { win });
    await api.putJson('/api/settings', { timeout: 30 });
    assert.equal(calls[0].options.headers['X-CSRF-Token'], token);
    assert.equal(calls[0].options.credentials, 'same-origin');
    await assert.rejects(api.putJson('https://other.test/steal', {}), /外部/);
    await assert.rejects(api.csrfHeaders('//other.test/upload'), /外部/);
    assert.equal(calls.length, 1);
});

test('expired auth clears the workspace without replaying mutations', async () => {
    let calls = 0;
    let clears = 0;
    let redirects = 0;
    const win = { location: { origin: 'https://workspace.test' }, __adminSession: {
        getCSRFToken: () => 'csrf', isAuthenticated: () => true, clear() { clears += 1; },
    }, __onAdminUnauthorized() { redirects += 1; } };
    const api = createTasksApi(async () => { calls += 1; return new Response('{}', { status: 401 }); }, { win });
    const { response } = await api.postJson('/api/tasks', {});
    assert.equal(response.status, 401);
    assert.equal(calls, 1);
    assert.equal(clears, 1);
    assert.equal(redirects, 1);
});

test('an aborted mutation is not sent after session refresh completes', async () => {
    const controller = new AbortController();
    let token = '';
    const win = { __adminSession: { getCSRFToken: () => token, isAuthenticated: () => true,
        async refresh() { token = 'csrf'; controller.abort(); },
    } };
    let calls = 0;
    const api = createTasksApi(async () => { calls += 1; return new Response('{}'); }, { win });
    await assert.rejects(api.postJson('/download', {}, { signal: controller.signal }), { name: 'AbortError' });
    assert.equal(calls, 0);
});
