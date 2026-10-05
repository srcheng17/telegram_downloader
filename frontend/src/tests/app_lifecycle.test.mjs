import test from 'node:test';
import assert from 'node:assert/strict';
import { createAppLifecycle } from '../shared/app_lifecycle.js';

function fixture(refresh) {
    const calls = [];
    const root = { hidden: false, replaceChildren() { calls.push('clear-dom'); } };
    const doc = { querySelector: () => null, querySelectorAll: () => [], getElementById: () => root };
    const win = { location: { pathname: '/', origin: 'https://local.test', replace: path => calls.push(path) },
        TelegraphDownloaderHome: { mount: () => calls.push('mount'), unmount: () => calls.push('unmount'), canLeave: () => false } };
    const session = { refresh, mount() {}, clear: () => calls.push('clear-session'), isAuthenticated: () => false };
    return { app: createAppLifecycle(win, doc, session), calls, root, win, doc };
}

test('navigation highlights the current URL after pending session refresh and before mounting', async () => {
    let resolve;
    let activeAtMount;
    const f = fixture(() => new Promise(done => { resolve = done; }));
    const links = ['/settings', '/logs'].map(href => {
        const link = { href, active: href === '/settings' };
        link.classList = { toggle(_name, active) { link.active = active; } };
        return link;
    });
    f.doc.querySelectorAll = selector => selector === '.main-nav .nav-link' ? links : [];
    f.win.location.pathname = '/settings';
    f.win.TelegraphDownloaderHome.mount = () => {
        activeAtMount = links.map(link => link.active);
        f.calls.push('mount');
    };

    const mounting = f.app.afterSwap({ detail: { target: { id: 'content' } } });
    assert.deepEqual(links.map(link => link.active), [true, false]);
    assert.deepEqual(f.calls, []);
    // The final navigation state must use the URL current when the async gate finishes.
    f.win.location.pathname = '/logs';
    resolve({ authenticated: true });
    await mounting;

    assert.deepEqual(links.map(link => link.active), [false, true]);
    assert.deepEqual(activeAtMount, [false, true]);
    assert.deepEqual(f.calls, ['mount']);
});

test('workspace stays hidden until auth verifies and clears DOM when restored session expired', async () => {
    let resolve;
    const f = fixture(() => new Promise(done => { resolve = done; }));
    const mounting = f.app.start();
    assert.equal(f.root.hidden, true);
    assert.deepEqual(f.calls, []);
    resolve({ authenticated: true });
    await mounting;
    assert.equal(f.root.hidden, false);
    assert.deepEqual(f.calls, ['mount']);
    const restoring = f.app.restore();
    assert.equal(f.root.hidden, true);
    resolve({ authenticated: false });
    await restoring;
    assert.equal(f.calls.includes('clear-dom'), true);
    assert.equal(f.calls.at(-1), '/auth/login');
});

test('late session refresh cannot mount an abandoned page and cancelled navigation retains draft', async () => {
    let resolve;
    const f = fixture(() => new Promise(done => { resolve = done; }));
    const mounting = f.app.start();
    let prevented = false;
    f.app.beforeRequest({ detail: { target: { id: 'content' } }, preventDefault() { prevented = true; } });
    assert.equal(prevented, true);
    assert.deepEqual(f.calls, []);
    f.app.pagehide();
    resolve({ authenticated: true });
    await mounting;
    assert.equal(f.calls.includes('mount'), false);
    assert.equal(f.root.hidden, true);
});


test('returning to the login page from BFCache restores its visible form', async () => {
    const f = fixture(async () => ({ authenticated: false }));
    f.win.location.pathname = '/auth/login';
    await f.app.start();
    f.app.pagehide();
    assert.equal(f.root.hidden, true);
    await f.app.restore();
    assert.equal(f.root.hidden, false);
    assert.equal(f.calls.includes('mount'), false);
});
