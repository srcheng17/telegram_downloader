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
    const win = {
        location: {
            origin: 'https://example.com',
            pathname: '/logs',
        },
    };
    const doc = {
        querySelectorAll() {
            return [
                { href: '/home', classList: homeClassList },
                { href: '/logs', classList: logsClassList },
            ];
        },
    };

    syncActiveNav(win, doc);

    assert.deepEqual(homeClassList.operations, [['active', false]]);
    assert.deepEqual(logsClassList.operations, [['active', true]]);
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
