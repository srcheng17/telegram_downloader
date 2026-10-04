import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { deflateSync } from 'node:zlib';
import test from 'node:test';
import { runTelegram } from '../telegram.mjs';
import { decodeQRPNG, renderQR } from '../qr_terminal.mjs';
import { CliError } from '../errors.mjs';

const cookie = 'a'.repeat(43);
const csrf = 'b'.repeat(43);
const attemptID = '1'.repeat(32);
const future = '2050-01-01T00:00:00Z';

function pngQR() {
    const width = 21;
    const ihdr = Buffer.alloc(13);
    ihdr.writeUInt32BE(width, 0);
    ihdr.writeUInt32BE(width, 4);
    ihdr[8] = 8;
    const rows = Buffer.alloc((width + 1) * width, 255);
    for (let y = 0; y < width; y += 1) {
        rows[y * (width + 1)] = 0;
        for (let x = 0; x < width; x += 1) {
            if (x < 7 && y < 7 && (x === 0 || y === 0 || x === 6 || y === 6)) rows[y * (width + 1) + x + 1] = 0;
        }
    }
    const chunk = (type, data) => {
        const header = Buffer.alloc(8);
        header.writeUInt32BE(data.length, 0);
        header.write(type, 4, 'ascii');
        return Buffer.concat([header, data, Buffer.alloc(4)]);
    };
    const body = Buffer.concat([
        Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
        chunk('IHDR', ihdr), chunk('IDAT', deflateSync(rows)), chunk('IEND', Buffer.alloc(0)),
    ]);
    return `data:image/png;base64,${body.toString('base64')}`;
}

function snapshot(state, seq, more = {}) {
    return { attempt_id: attemptID, seq, revision: seq, state, expires_at: future, ...more };
}

async function fixture(handler) {
    const calls = [];
    const app = createServer(async (req, res) => {
        let body = '';
        for await (const chunk of req) body += chunk;
        calls.push({ method: req.method, path: req.url, body, headers: req.headers });
        if (req.url === '/api/auth/session') return respond(res, { authenticated: true, csrf_token: csrf, expires_at: future });
        if (req.url === '/api/client-contract') return respond(res, { protocol_version: 1, client: 'mediactl' });
        await handler(req, res, body);
    });
    app.listen(0, '127.0.0.1');
    await once(app, 'listening');
    const origin = `http://127.0.0.1:${app.address().port}`;
    const store = { load: async () => ({ origin, cookie, csrf, expiresAt: future }), save: async () => {} };
    return { app, calls, store };
}

function respond(res, data, status = 200) {
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(data));
}

const options = (server, more = {}) => ({ store: server.store, allowInsecureLoopback: true, ...more });

test('QR decoder renders bounded PNG only to the terminal representation', () => {
    const qr = pngQR();
    const image = decodeQRPNG(qr);
    assert.deepEqual({ width: image.width, height: image.height }, { width: 21, height: 21 });
    assert.equal(image.pixels[0], 1);
    const display = renderQR(qr, 80);
    assert.match(display, /█/u);
    assert.doesNotMatch(display, /data:image|base64/);
    assert.throws(() => renderQR(qr, 40), { code: 'terminal_too_narrow' });
    assert.throws(() => decodeQRPNG('data:image/png;base64,AAAA'), { code: 'invalid_qr' });
    assert.throws(() => decodeQRPNG(`data:image/png;base64,${'A'.repeat(24_001)}`), { code: 'invalid_qr' });
});

test('noninteractive Telegram login does not read session or create an attempt', async () => {
    const store = { load: async () => { throw new Error('session must not be read'); } };
    await assert.rejects(runTelegram('login', { store, interactive: false }), { code: 'interaction_required' });
    await assert.rejects(runTelegram('login', { store, interactive: true, json: true }), { code: 'interaction_required' });
});

test('account status and cancel strip QR material from machine output', async (t) => {
    const qr = pngQR();
    const server = await fixture((req, res) => {
        if (req.url === '/api/telegram/account') return respond(res, { state: 'unverified', revision: 2, busy: true, max_source_bytes: 524288000, attempt: snapshot('waiting_qr', 2, { qr, qr_expires_at: future }) });
        if (req.url === `/api/telegram/login-attempts/${attemptID}/cancel`) return respond(res, snapshot('cancelled', 3, { qr }));
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const status = await runTelegram('status', options(server));
    const cancelled = await runTelegram('cancel', options(server, { attemptId: attemptID }));
    assert.equal(status.attempt.state, 'waiting_qr');
    assert.equal(cancelled.attempt.state, 'cancelled');
    assert.doesNotMatch(JSON.stringify({ status, cancelled }), /data:image|base64/);
    const write = server.calls.find((call) => call.path.endsWith('/cancel'));
    assert.equal(write.headers['x-csrf-token'], csrf);
});

test('interactive login prompts once per password state sequence and verifies account', async (t) => {
    const qr = pngQR();
    let current = snapshot('waiting_qr', 1, { qr, qr_expires_at: future });
    const server = await fixture((req, res) => {
        if (req.url === '/api/telegram/login-attempts' && req.method === 'POST') return respond(res, current, 202);
        if (req.url === `/api/telegram/login-attempts/${attemptID}`) return respond(res, current);
        if (req.url === `/api/telegram/login-attempts/${attemptID}/password`) return respond(res, { accepted: true }, 202);
        if (req.url === '/api/telegram/account/verify') return respond(res, { state: 'connected', revision: 4, busy: false, max_source_bytes: 524288000, verified_at: future });
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    const terminal = { shown: [], notes: [], cleared: 0, closed: false,
        show(value) { this.shown.push(value); }, clear() { this.cleared += 1; }, note(value) { this.notes.push(value); }, async close() { this.closed = true; } };
    let sleeps = 0;
    const secrets = ['incorrect', 'correct'];
    const result = await runTelegram('login', options(server, { interactive: true }), {
        openTerminal: async () => terminal,
        secretReader: async () => secrets.shift(),
        sleep: async () => {
            sleeps += 1;
            if (sleeps === 1) current = snapshot('password_required', 2);
            if (sleeps === 3) current = snapshot('password_required', 3, { code: 'password_invalid' });
            if (sleeps === 4) current = snapshot('connected', 4);
        },
    });
    assert.equal(result.state, 'connected');
    assert.equal(terminal.shown.length, 1);
    assert.equal(terminal.closed, true);
    assert.match(terminal.notes.join(' '), /密码无效/);
    assert.deepEqual(server.calls.filter((call) => call.path.endsWith('/password')).map((call) => JSON.parse(call.body).password), ['incorrect', 'correct']);
    assert.equal(server.calls.some((call) => call.path.endsWith('/cancel')), false);
    assert.doesNotMatch(JSON.stringify(result), /incorrect|correct|data:image/);
});

test('login interruption cancels the attempt and clears terminal state', async (t) => {
    const server = await fixture((req, res) => {
        if (req.url === '/api/telegram/login-attempts' && req.method === 'POST') return respond(res, snapshot('password_required', 1), 202);
        if (req.url === `/api/telegram/login-attempts/${attemptID}/cancel`) return respond(res, snapshot('cancelled', 2));
        return respond(res, {}, 404);
    });
    t.after(() => server.app.close());
    let closed = false;
    await assert.rejects(runTelegram('login', options(server, { interactive: true }), {
        openTerminal: async () => ({ clear() {}, show() {}, note() {}, async close() { closed = true; } }),
        secretReader: async () => { throw new CliError('cancelled', '操作已取消。'); },
    }), { code: 'cancelled' });
    assert.equal(closed, true);
    assert.equal(server.calls.filter((call) => call.path.endsWith('/cancel')).length, 1);
});
