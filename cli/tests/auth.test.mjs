import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import test from 'node:test';
import { runAuth } from '../auth.mjs';
import { main, parseArgs } from '../mediactl.mjs';

const preCookie = 'p'.repeat(43);
const authCookie = 'a'.repeat(43);
const preCSRF = 'c'.repeat(43);
const authCSRF = 'd'.repeat(43);

function memoryStore() {
    let saved = null;
    return { load: async () => saved, save: async (value) => { saved = value; }, clear: async () => { saved = null; }, peek: () => saved };
}

async function authServer() {
    const calls = [];
    const app = createServer(async (req, res) => {
        let body = '';
        for await (const chunk of req) body += chunk;
        calls.push({ method: req.method, path: req.url, cookie: req.headers.cookie, csrf: req.headers['x-csrf-token'], origin: req.headers.origin, body });
        const current = req.headers.cookie === `td_admin_session=${authCookie}`;
        if (req.url === '/api/client-contract') {
            res.end(JSON.stringify({ protocol_version: 1, client: 'mediactl' }));
        } else if (req.url === '/api/auth/session') {
            if (current) res.end(JSON.stringify({ authenticated: true, csrf_token: authCSRF, expires_at: 'future' }));
            else {
                res.setHeader('Set-Cookie', `td_admin_session=${preCookie}; Path=/; HttpOnly`);
                res.end(JSON.stringify({ authenticated: false, csrf_token: preCSRF, expires_at: 'future' }));
            }
        } else if (req.url === '/api/auth/login') {
            if (req.headers.cookie !== `td_admin_session=${preCookie}` || req.headers['x-csrf-token'] !== preCSRF || JSON.parse(body).password !== 'secret12345678') {
                res.statusCode = 403;
                res.end(JSON.stringify({ code: 'csrf_rejected' }));
            } else {
                res.setHeader('Set-Cookie', `td_admin_session=${authCookie}; Path=/; HttpOnly`);
                res.end(JSON.stringify({ authenticated: true, csrf_token: authCSRF, expires_at: 'future' }));
            }
        } else if (req.url === '/api/auth/logout' || req.url === '/api/auth/password') {
            assert.equal(req.headers.cookie, `td_admin_session=${authCookie}`);
            assert.equal(req.headers['x-csrf-token'], authCSRF);
            res.end(JSON.stringify({ authenticated: false }));
        } else { res.statusCode = 404; res.end('{}'); }
    });
    app.listen(0, '127.0.0.1');
    await once(app, 'listening');
    return { app, calls, origin: `http://127.0.0.1:${app.address().port}` };
}

test('noninteractive and JSON secret commands do not read store or contact server', async () => {
    const store = { async load() { throw new Error('should not load'); } };
    for (const action of ['login', 'password-change']) {
        await assert.rejects(runAuth(action, { server: 'https://example.test', store, interactive: false }), { code: 'interaction_required' });
        await assert.rejects(runAuth(action, { server: 'https://example.test', store, interactive: true, json: true }), { code: 'interaction_required' });
    }
    await assert.rejects(runAuth('login', { server: 'https://example.test', store, interactive: true, file: '/tmp/secret' }), { code: 'invalid_input' });
});

test('login rotates preauth cookie and verifies saved authenticated session', async (t) => {
    const { app, calls, origin } = await authServer();
    t.after(() => app.close());
    const store = memoryStore();
    const state = await runAuth('login', { server: origin, allowInsecureLoopback: true, store, interactive: true, secretReader: async () => 'secret12345678' });
    assert.equal(state.authenticated, true);
    assert.equal(store.peek().cookie, authCookie);
    assert.equal(calls.length, 3);
    assert.equal(calls[1].origin, origin);
    assert.equal(calls[1].csrf, preCSRF);
    assert.equal(calls[2].cookie, `td_admin_session=${authCookie}`);
    assert.deepEqual(await runAuth('status', { allowInsecureLoopback: true, store }), { authenticated: true, expires_at: 'future' });
    await runAuth('logout', { allowInsecureLoopback: true, store });
    assert.equal(store.peek(), null);
    assert.equal(calls.at(-1).path, '/api/auth/logout');
});

test('password change requires hidden prompts and revokes local session after server success', async (t) => {
    const { app, calls, origin } = await authServer();
    t.after(() => app.close());
    const store = memoryStore();
    await store.save({ origin, cookie: authCookie, csrf: authCSRF });
    const values = ['old-secret', 'new-secret-123', 'new-secret-123'];
    const state = await runAuth('password-change', { allowInsecureLoopback: true, store, interactive: true, secretReader: async () => values.shift() });
    assert.deepEqual(state, { authenticated: false, password_changed: true });
    assert.equal(store.peek(), null);
    assert.equal(calls.at(-1).method, 'PUT');
    assert.equal(JSON.parse(calls.at(-1).body).new_password, 'new-secret-123');
});

test('entry reports stable JSON and does not echo rejected secret argv', async () => {
    const output = [];
    const errors = [];
    const stdout = { write: (text) => output.push(text) };
    const stderr = { write: (text) => errors.push(text) };
    assert.equal(await main(['--json', 'auth', 'status'], { stdout, stderr, store: memoryStore() }), 0);
    assert.deepEqual(JSON.parse(output[0]), { ok: true, code: 'ok', data: { authenticated: false } });
    assert.equal(await main(['--json', 'auth', 'login', '--password', 'TOP_SECRET'], { stdout, stderr }), 2);
    assert.deepEqual(JSON.parse(errors[0]), { ok: false, code: 'invalid_input', message: '命令参数无效。' });
    assert.doesNotMatch(errors.join(''), /TOP_SECRET/);
    assert.equal(parseArgs(['auth', 'status']).action, 'status');
    output.length = 0;
    assert.equal(await main(['--json', '--version'], { stdout, stderr }), 0);
    assert.match(JSON.parse(output[0]).data.version, /^\d+\.\d+\.\d+/);
    output.length = 0;
    assert.equal(await main(['tasks', 'create-url', '--url', 'https://telegra.ph/example'], {
        stdout, stderr, runTasksCommand: async () => ({ task: { id: 'task-1', status: 'SUCCEEDED' }, needs_confirmation: true }),
    }), 0);
    assert.match(output[0], /未创建新任务/);
});
