import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import test from 'node:test';
import { createClient, parseSessionCookie, validateServerURL } from '../http.mjs';

const token = 'a'.repeat(43);
const csrf = 'b'.repeat(43);

async function server(handler) {
    const app = createServer(handler);
    app.listen(0, '127.0.0.1');
    await once(app, 'listening');
    return { app, origin: `http://127.0.0.1:${app.address().port}` };
}

test('server URL requires HTTPS or explicit loopback HTTP', () => {
    assert.equal(validateServerURL('https://example.test/'), 'https://example.test');
    assert.throws(() => validateServerURL('http://127.0.0.1:3000'), { code: 'insecure_server' });
    assert.equal(validateServerURL('http://127.0.0.1:3000', { allowInsecureLoopback: true }), 'http://127.0.0.1:3000');
    assert.equal(validateServerURL('http://[::1]:3000', { allowInsecureLoopback: true }), 'http://[::1]:3000');
    assert.throws(() => validateServerURL('http://example.test', { allowInsecureLoopback: true }), { code: 'insecure_server' });
    assert.throws(() => validateServerURL('https://user:secret@example.test'), { code: 'invalid_server' });
    assert.throws(() => validateServerURL('https://example.test/path'), { code: 'invalid_server' });
    assert.throws(() => validateServerURL('https://example.test/#fragment'), { code: 'invalid_server' });
});

test('session cookie parser picks only the administrator cookie', () => {
    assert.equal(parseSessionCookie([`other=x; Path=/`, `td_admin_session=${token}; Path=/; HttpOnly`]), token);
    assert.equal(parseSessionCookie(['td_admin_session=; Max-Age=-1']), '');
    assert.equal(parseSessionCookie(['other=x']), undefined);
    assert.throws(() => parseSessionCookie(['td_admin_session=malformed']), { code: 'invalid_session' });
});

test('request sends same-origin Origin, cookie and CSRF; strips raw server error text', async (t) => {
    const requests = [];
    const { app, origin } = await server((req, res) => {
        requests.push({ url: req.url, cookie: req.headers.cookie, origin: req.headers.origin, csrf: req.headers['x-csrf-token'] });
        res.writeHead(req.url === '/error' ? 403 : 200, { 'Content-Type': 'application/json' });
        res.end(req.url === '/error' ? JSON.stringify({ code: 'csrf_rejected', message: 'SECRET IN ERROR' }) : req.url === '/api/client-contract' ? JSON.stringify({ protocol_version: 1, client: 'mediactl' }) : JSON.stringify({ ok: true }));
    });
    t.after(() => app.close());
    const client = createClient(origin, { allowInsecureLoopback: true });
    await client.request('POST', '/api/test', { cookie: token, csrf, body: { value: 'plain data' } });
    assert.deepEqual(requests[0], { url: '/api/client-contract', cookie: `td_admin_session=${token}`, origin: undefined, csrf: undefined });
    assert.deepEqual(requests[1], { url: '/api/test', cookie: `td_admin_session=${token}`, origin, csrf });
    await assert.rejects(client.request('POST', '/error', { cookie: token, csrf }), (error) => {
        assert.equal(error.code, 'csrf_rejected');
        assert.doesNotMatch(error.message, /SECRET/);
        return true;
    });
    await assert.rejects(client.request('GET', '//other.test/path', { cookie: token }), { code: 'invalid_path' });
});

test('business writes require a fresh matching client contract before side effects', async (t) => {
    let contract = { protocol_version: 1, client: 'mediactl' };
    let contractHits = 0; let writes = 0;
    const { app, origin } = await server((req, res) => {
        res.setHeader('Content-Type', 'application/json');
        if (req.url === '/api/client-contract') {
            contractHits++;
            res.statusCode = contract === null ? 404 : 200;
            res.end(JSON.stringify(contract || { code: 'not_found', message: 'SECRET UPSTREAM' }));
            return;
        }
        writes++; res.end('{}');
    });
    t.after(() => app.close());
    const client = createClient(origin, { allowInsecureLoopback: true });
    await client.request('POST', '/api/write', { cookie: token, csrf, body: { title: 'sensitive fixture' } });
    assert.equal(writes, 1);
    contract = { protocol_version: 2, client: 'mediactl' };
    await assert.rejects(client.request('PUT', '/api/write', { cookie: token, csrf, body: {} }), { code: 'incompatible_server' });
    contract = { protocol_version: '1', client: 'mediactl' };
    await assert.rejects(client.request('PATCH', '/api/write', { cookie: token, csrf, body: {} }), { code: 'incompatible_server' });
    contract = null;
    await assert.rejects(client.request('DELETE', '/api/write', { cookie: token, csrf }), (error) => {
        assert.equal(error.code, 'incompatible_server');
        assert.doesNotMatch(error.message, /SECRET/);
        return true;
    });
    assert.equal(writes, 1);
    assert.equal(contractHits, 4);
    // Login/logout remain available even when the protected contract is absent.
    await client.request('POST', '/api/auth/login', { cookie: token, csrf, body: {} });
    await client.request('POST', '/api/auth/logout', { cookie: token, csrf });
    assert.equal(writes, 3);
    assert.equal(contractHits, 4);
});

test('redirect is never followed or sent cookies to a new origin', async (t) => {
    let secondHits = 0;
    const second = await server((_req, res) => { secondHits += 1; res.end('unexpected'); });
    t.after(() => second.app.close());
    const first = await server((_req, res) => {
        res.writeHead(302, { Location: `${second.origin}/stolen`, 'Content-Type': 'application/json' });
        res.end('{}');
    });
    t.after(() => first.app.close());
    const client = createClient(first.origin, { allowInsecureLoopback: true });
    await assert.rejects(client.request('GET', '/api/test', { cookie: token }), { code: 'unsafe_redirect' });
    assert.equal(secondHits, 0);
});
