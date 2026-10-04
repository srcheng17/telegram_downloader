import test from 'node:test';
import assert from 'node:assert/strict';
import { createAdminSession } from '../shared/admin_session.js';
import { createRequestScope } from '../settings/integrations_api.js';

const token = 'x'.repeat(43);
const response = (payload) => ({ ok: true, json: async () => payload });
function fixture(fetchImpl) { return createAdminSession({ win: { location: { assign() {} } }, doc: { querySelectorAll: () => [], getElementById: () => null }, fetchImpl }); }

test('session refresh deduplicates and clear rejects an old response', async () => {
    let resolve;
    let calls = 0;
    const session = fixture(() => { calls += 1; return new Promise((done) => { resolve = done; }); });
    const first = session.refresh();
    assert.equal(session.refresh(), first);
    session.clear();
    resolve(response({ authenticated: true, csrf_token: token }));
    await assert.rejects(first, { name: 'AbortError' });
    assert.equal(session.getCSRFToken(), '');
    assert.equal(calls, 1);
});

test('login uses preauth CSRF and accepts only the rotated response', async () => {
    const requests = [];
    const session = fixture(async (url, options) => {
        requests.push({ url, options });
        return response({ authenticated: url.endsWith('/login'), csrf_token: url.endsWith('/login') ? 'y'.repeat(43) : token });
    });
    await session.login('synthetic-long-password');
    assert.equal(requests[1].options.headers['X-CSRF-Token'], token);
    assert.equal(session.getCSRFToken(), 'y'.repeat(43));
    assert.equal(session.isAuthenticated(), true);
    session.clear();
    assert.equal(session.isAuthenticated(), false);
});

test('settings requests cannot apply after editing, replacement or unmount', () => {
    const scope = createRequestScope();
    const first = scope.start('models');
    const next = scope.start('models');
    assert.equal(first.current(), false);
    assert.equal(first.signal.aborted, true);
    assert.equal(next.current(), true);
    scope.invalidate();
    assert.equal(next.current(), false);
    const last = scope.start('save');
    scope.dispose();
    assert.equal(last.current(), false);
});

test('clearing session aborts a pending login and rejects its late response', async () => {
    let loginSignal;
    let finish;
    const session = fixture(async (url, options) => {
        if (url.endsWith('/session')) return response({ authenticated: false, csrf_token: token });
        loginSignal = options.signal;
        return new Promise((resolve) => { finish = resolve; });
    });
    const login = session.login('synthetic-long-password');
    await new Promise((resolve) => setImmediate(resolve));
    session.clear();
    assert.equal(loginSignal.aborted, true);
    finish(response({ authenticated: true, csrf_token: 'y'.repeat(43) }));
    await assert.rejects(login, { name: 'AbortError' });
    assert.equal(session.getCSRFToken(), '');
});

test('an already authenticated login page returns to the workspace', async () => {
    const destinations = [];
    const form = { addEventListener() {}, removeEventListener() {} };
    const doc = { querySelectorAll: () => [], getElementById: id => id === 'admin-login-form' ? form : null };
    const session = createAdminSession({
        win: { location: { replace: path => destinations.push(path) } }, doc,
        fetchImpl: async () => response({ authenticated: true, csrf_token: token }),
    });
    session.mount();
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(destinations, ['/']);
    session.unmount();
});
