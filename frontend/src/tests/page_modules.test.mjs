import test from 'node:test';
import assert from 'node:assert/strict';

import { mountPageModules, syncActiveNav, unmountPageModules } from '../shared/page_modules.js';

function createClassList() {
    const operations = [];
    return {
        operations,
        toggle(name, active) {
            operations.push([name, active]);
        },
    };
}

test('syncActiveNav marks the current path as active', () => {
    const homeClassList = createClassList();
    const logsClassList = createClassList();
    const homeAttributes = new Map([['aria-current', 'page']]);
    const logsAttributes = new Map();
    const win = {
        location: {
            origin: 'https://example.com',
            pathname: '/logs',
        },
    };
    const doc = {
        querySelectorAll() {
            return [
                { href: '/home', classList: homeClassList, setAttribute: (key, value) => homeAttributes.set(key, value), removeAttribute: key => homeAttributes.delete(key) },
                { href: '/logs', classList: logsClassList, setAttribute: (key, value) => logsAttributes.set(key, value), removeAttribute: key => logsAttributes.delete(key) },
            ];
        },
    };

    syncActiveNav(win, doc);

    assert.deepEqual(homeClassList.operations, [['active', false]]);
    assert.deepEqual(logsClassList.operations, [['active', true]]);
    assert.equal(homeAttributes.has('aria-current'), false);
    assert.equal(logsAttributes.get('aria-current'), 'page');
});

test('mountPageModules and unmountPageModules call page lifecycle hooks when present', () => {
    const calls = [];
    const win = {
        TelegraphDownloaderHome: {
            mount() {
                calls.push('home:mount');
            },
            unmount() {
                calls.push('home:unmount');
            },
        },
        TelegraphDownloaderLogs: {
            mount() {
                calls.push('logs:mount');
            },
            unmount() {
                calls.push('logs:unmount');
            },
        },
        TelegraphDownloaderSettings: {
            mount() {
                calls.push('settings:mount');
            },
            unmount() {
                calls.push('settings:unmount');
            },
        },
        // The settings Connections tab now owns this module's lifecycle.
        TelegraphDownloaderTelegram: {
            mount() { assert.fail('global Telegram mount bypassed settings tab ownership'); },
            unmount() { assert.fail('global Telegram unmount bypassed settings tab ownership'); },
        },
    };

    mountPageModules(win);
    unmountPageModules(win);

    assert.deepEqual(calls, [
        'home:mount',
        'logs:mount',
        'settings:mount',
        'home:unmount',
        'logs:unmount',
        'settings:unmount',
    ]);
});


test('htmx retains page modules for rejected swaps and remounts history restores', async () => {
    const previousWindow = globalThis.window;
    const previousDocument = globalThis.document;
    const listeners = {};
    const calls = [];
    globalThis.window = {
        location: { origin: 'http://localhost', pathname: '/logs' },
        addEventListener() {},
        __adminSession: { mount() {}, clear() {}, async refresh() { return { authenticated: true }; }, isAuthenticated: () => true },
        TelegraphDownloaderLogs: {
            mount() { calls.push('mount'); },
            unmount() { calls.push('unmount'); },
        },
    };
    globalThis.document = {
        querySelectorAll() { return []; },
        querySelector() { return null; },
        getElementById() { return null; },
        addEventListener(name, handler) { listeners[name] = handler; },
        body: { addEventListener(name, handler) { listeners[name] = handler; } },
    };
    try {
        await import('../app.js');
        assert.equal(Object.hasOwn(globalThis.window, 'TelegraphDownloaderTelegram'), false);
        await listeners.DOMContentLoaded();
        listeners['htmx:beforeSwap']({ detail: { target: { id: 'content' }, shouldSwap: false } });
        assert.deepEqual(calls, ['mount']);
        listeners['htmx:beforeSwap']({ defaultPrevented: true, detail: { target: { id: 'content' }, shouldSwap: true } });
        assert.deepEqual(calls, ['mount']);
        listeners['htmx:beforeSwap']({ detail: { target: { id: 'content' }, shouldSwap: true } });
        assert.deepEqual(calls, ['mount', 'unmount']);
        assert.equal(typeof listeners['htmx:historyRestore'], 'function');
        await listeners['htmx:historyRestore']();
        assert.deepEqual(calls, ['mount', 'unmount', 'unmount', 'mount']);
    } finally {
        globalThis.window = previousWindow;
        globalThis.document = previousDocument;
    }
});
