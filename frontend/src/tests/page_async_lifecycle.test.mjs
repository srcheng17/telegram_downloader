import test from 'node:test';
import assert from 'node:assert/strict';
import { createHomeModule } from '../home/index.js';

function deferred() {
    let resolve;
    const promise = new Promise((done) => { resolve = done; });
    return { promise, resolve };
}

async function flush() {
    await new Promise((resolve) => setImmediate(resolve));
}

function homeFixture(fetchImpl, hidden = false) {
    const nodes = {
        'summary-panel': {},
        'summary-active': { textContent: '-' },
    };
    const listeners = new Map();
    const timers = new Map();
    let timerID = 0;
    const doc = {
        visibilityState: hidden ? 'hidden' : 'visible',
        getElementById: (id) => nodes[id] || null,
        querySelector: () => null,
        addEventListener: (name, callback) => listeners.set(name, callback),
        removeEventListener: (name) => listeners.delete(name),
    };
    const win = {
        fetch: fetchImpl,
        addEventListener() {}, removeEventListener() {},
        setTimeout(callback, delay) { timers.set(++timerID, { callback, delay }); return timerID; },
        clearTimeout: (id) => timers.delete(id),
        // Kept to expose the old implementation's overlapping interval in this regression.
        setInterval(callback, delay) { timers.set(++timerID, { callback, delay }); return timerID; },
    };
    return { doc, win, nodes, listeners, timers, module: createHomeModule(win, doc) };
}

test('home summary schedules only after a slow request and ignores responses after unmount', async () => {
    const request = deferred();
    let signal;
    const fixture = homeFixture((url, options) => { signal = options.signal; return request.promise; });
    fixture.module.mount();
    assert.equal(fixture.timers.size, 0);
    fixture.module.unmount();
    assert.equal(signal.aborted, true);
    request.resolve(new Response(JSON.stringify({ active_tasks: 9 }), { status: 200 }));
    await flush();
    assert.equal(fixture.nodes['summary-active'].textContent, '-');
    assert.equal(fixture.timers.size, 0);
});

test('home summary pauses while hidden and resumes on visibility without duplicate mount requests', async () => {
    let requests = 0;
    const fixture = homeFixture(async () => {
        requests += 1;
        return new Response(JSON.stringify({ active_tasks: 2 }), { status: 200 });
    }, true);
    fixture.module.mount();
    fixture.module.mount();
    await flush();
    assert.equal(requests, 0);
    assert.equal(fixture.timers.size, 0);
    fixture.doc.visibilityState = 'visible';
    fixture.listeners.get('visibilitychange')();
    await flush();
    assert.equal(requests, 1);
    assert.equal(fixture.nodes['summary-active'].textContent, '2');
    assert.equal(fixture.timers.size, 1);
    fixture.doc.visibilityState = 'hidden';
    fixture.listeners.get('visibilitychange')();
    assert.equal(fixture.timers.size, 0);
    fixture.module.unmount();
    assert.equal(fixture.listeners.has('visibilitychange'), false);
});

async function settingsFixture(fetchImpl) {
    globalThis.window = {};
    globalThis.document = {};
    const { createSettingsModule } = await import('../settings.js');
    delete globalThis.window;
    delete globalThis.document;
    const listeners = new Map();
    const inputs = { '#timeout': { value: '30' }, '#retries': { value: '3' }, '#image_concurrency': { value: '2' }, 'button[type="submit"]': { disabled: false } };
    const radio = { value: 'browser', checked: true };
    const form = {
        querySelector: (selector) => selector.includes(':checked') ? radio : inputs[selector] || null,
        querySelectorAll: () => [radio],
        addEventListener: (name, callback) => listeners.set(name, callback),
        removeEventListener: (name) => listeners.delete(name),
    };
    const win = { fetch: fetchImpl };
    const doc = { querySelector: () => form, getElementById: () => null };
    return { win, inputs, form, listeners, module: createSettingsModule(win, doc) };
}

test('settings hydration cannot overwrite edits or stale global state after unmount', async () => {
    const request = deferred();
    let signal;
    const fixture = await settingsFixture((url, options) => { signal = options.signal; return request.promise; });
    fixture.module.mount();
    fixture.inputs['#timeout'].value = '45';
    fixture.listeners.get('input')();
    assert.equal(signal.aborted, true);
    request.resolve(new Response(JSON.stringify({ timeout: 30, download_action_mode: 'komga_copy' }), { status: 200 }));
    await flush();
    assert.equal(fixture.inputs['#timeout'].value, '45');
    assert.equal(fixture.win.__telegraphSettingsState, undefined);
    fixture.module.unmount();
});

test('settings unmount aborts hydration and ignores even a late successful response', async () => {
    const request = deferred();
    let signal;
    const fixture = await settingsFixture((url, options) => { signal = options.signal; return request.promise; });
    fixture.module.mount();
    fixture.module.unmount();
    assert.equal(signal.aborted, true);
    request.resolve(new Response(JSON.stringify({ timeout: 99, download_action_mode: 'komga_copy' }), { status: 200 }));
    await flush();
    assert.equal(fixture.inputs['#timeout'].value, '30');
    assert.equal(fixture.win.__telegraphSettingsState, undefined);
});


test('settings save guards duplicate requests and preserves edits made while saving', async () => {
    const request = deferred();
    let saves = 0;
    const fixture = await settingsFixture((url, options) => {
        if (options.method === 'PUT') { saves += 1; return request.promise; }
        return Promise.resolve(new Response(JSON.stringify({ timeout: 30, retries: 3, image_concurrency: 2, download_action_mode: 'browser' }), { status: 200 }));
    });
    fixture.module.mount();
    await flush();
    fixture.inputs['#timeout'].value = '45';
    fixture.listeners.get('input')();
    const event = { preventDefault() {}, currentTarget: fixture.form };
    const saving = fixture.listeners.get('submit')(event);
    await fixture.listeners.get('submit')(event);
    assert.equal(saves, 1);
    assert.equal(fixture.inputs['button[type="submit"]'].disabled, true);
    fixture.inputs['#timeout'].value = '60';
    fixture.listeners.get('input')();
    request.resolve(new Response(JSON.stringify({ timeout: 45, download_action_mode: 'komga_copy' }), { status: 200 }));
    await saving;
    assert.equal(fixture.inputs['#timeout'].value, '60');
    assert.equal(fixture.inputs['button[type="submit"]'].disabled, false);
    assert.equal(fixture.win.__telegraphSettingsState.downloadActionMode, 'komga_copy');
    fixture.module.unmount();
});


test('settings restores a cached disabled save button with no in-flight save', async () => {
    const fixture = await settingsFixture(async () => new Response(JSON.stringify({ timeout: 30 }), { status: 200 }));
    fixture.inputs['button[type="submit"]'].disabled = true;
    fixture.module.mount();
    assert.equal(fixture.inputs['button[type="submit"]'].disabled, false);
    fixture.module.unmount();
});
