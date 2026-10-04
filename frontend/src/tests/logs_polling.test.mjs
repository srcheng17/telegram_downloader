import test from 'node:test';
import assert from 'node:assert/strict';
import { createLogsModule } from '../logs/index.js';

function element(tagName = 'div') {
    const listeners = new Map();
    const classes = new Set();
    let html = '';
    const node = {
        value: '', textContent: '', children: [], style: {}, disabled: false,
        classList: {
            add: (...names) => names.forEach((name) => classes.add(name)),
            remove: (...names) => names.forEach((name) => classes.delete(name)),
            contains: (name) => classes.has(name),
        },
        addEventListener: (name, handler) => listeners.set(name, handler),
        removeEventListener: (name) => listeners.delete(name),
        emit: (name) => listeners.get(name)?.({ preventDefault() {} }),
        appendChild(child) {
            this.children.push(child);
            if (tagName === 'select' && this.children.length === 1) this.value = child.value;
        },
        set innerHTML(value) {
            html = value;
            this.children = [];
            if (tagName === 'select') this.value = '';
        },
        get innerHTML() { return html; },
    };
    return node;
}

async function flush() {
    await new Promise((resolve) => setImmediate(resolve));
}

function logsFixture(t) {
    const nodes = Object.fromEntries([
        'logs-page', 'logs-filter-form', 'query-filter', 'clear-filters',
        'prev-page', 'next-page', 'page-info', 'log-body', 'logs-feedback', 'summary-active-count',
    ].map((id) => [id, element()]));
    nodes['status-filter'] = element('select');
    const listeners = new Map();
    const timers = new Map();
    let timerID = 0;
    const setTimer = (callback, delay) => {
        timers.set(++timerID, { callback, delay });
        return timerID;
    };
    const clearTimer = (id) => timers.delete(id);
    // Also capture the original module's global timers to reproduce its old race.
    t.mock.method(globalThis, 'setTimeout', setTimer);
    t.mock.method(globalThis, 'clearTimeout', clearTimer);
    const requests = [];
    const win = {
        fetch(url, options) {
            return new Promise((resolve, reject) => requests.push({ url, signal: options.signal, resolve, reject }));
        },
        setTimeout: setTimer, clearTimeout: clearTimer,
        addEventListener() {}, removeEventListener() {},
    };
    const doc = {
        visibilityState: 'visible',
        getElementById: (id) => nodes[id] || null,
        createElement: element,
        addEventListener: (name, handler) => listeners.set(name, handler),
        removeEventListener: (name) => listeners.delete(name),
    };
    const module = createLogsModule(win, doc);
    t.after(() => module.unmount());
    return {
        module, win, doc, nodes, requests, timers, listeners,
        async respond(index, payload = {}, status = 200) {
            const request = requests[index];
            const query = new URL(request.url, 'http://localhost').searchParams;
            request.resolve(new Response(JSON.stringify({
                logs: [], page: Number(query.get('page')), total_pages: 3,
                filters: { status: query.get('status') || '', q: query.get('q') || '' },
                status_catalog: { RUNNING: { label: '运行中' }, FAILED: { label: '失败' }, SUCCEEDED: { label: '完成' } },
                summary: { active_tasks: 1 },
                ...payload,
            }), { status }));
            await flush();
        },
        fireTimers() {
            for (const [id, timer] of [...timers]) {
                timers.delete(id);
                timer.callback();
            }
        },
    };
}

test('logs polling preserves draft query and status while requesting applied filters', async (t) => {
    const f = logsFixture(t);
    f.module.mount();
    await f.respond(0);
    assert.equal(f.nodes['status-filter'].children.length, 4);
    f.nodes['query-filter'].value = 'applied';
    f.nodes['status-filter'].value = 'FAILED';
    f.nodes['logs-filter-form'].emit('submit');
    await f.respond(1);

    f.nodes['query-filter'].value = 'draft still being typed';
    f.nodes['status-filter'].value = 'SUCCEEDED';
    f.fireTimers();
    const query = new URL(f.requests[2].url, 'http://localhost').searchParams;
    assert.equal(query.get('q'), 'applied');
    assert.equal(query.get('status'), 'FAILED');
    await f.respond(2);
    assert.equal(f.nodes['query-filter'].value, 'draft still being typed');
    assert.equal(f.nodes['status-filter'].value, 'SUCCEEDED');

    f.nodes['logs-filter-form'].emit('submit');
    assert.match(f.requests[3].url, /status=SUCCEEDED/);
    assert.match(f.requests[3].url, /q=draft\+still\+being\+typed/);
    await f.respond(3);
    f.nodes['clear-filters'].emit('click');
    assert.equal(f.nodes['query-filter'].value, '');
    assert.equal(f.nodes['status-filter'].value, '');
    assert.equal(new URL(f.requests[4].url, 'http://localhost').searchParams.has('q'), false);
    await f.respond(4);
});

test('logs slow pagination clears the previous timer and cannot be interrupted by it', async (t) => {
    const f = logsFixture(t);
    f.module.mount();
    await f.respond(0);
    assert.equal(f.timers.size, 1);
    f.nodes['next-page'].emit('click');
    assert.match(f.requests[1].url, /page=2&/);
    f.fireTimers();
    assert.equal(f.requests.length, 2, 'old timer started a competing page request');
    assert.equal(f.requests[1].signal.aborted, false);
    assert.equal(f.timers.size, 0, 'in-flight page request must own the next scheduling decision');
    await f.respond(1);
    assert.equal(f.nodes['page-info'].textContent, '第 2 / 3 页');
    assert.equal(f.timers.size, 1);
});

test('logs ignores superseded and unmounted responses without reviving timers', async (t) => {
    const f = logsFixture(t);
    f.module.mount();
    f.module.mount();
    assert.equal(f.requests.length, 1);
    f.nodes['query-filter'].value = 'new query';
    f.nodes['logs-filter-form'].emit('submit');
    assert.equal(f.requests[0].signal.aborted, true);
    await f.respond(0, { page: 3, summary: { active_tasks: 99 } });
    assert.equal(f.nodes['summary-active-count'].textContent, '');
    assert.equal(f.timers.size, 0);
    await f.respond(1);
    assert.equal(f.nodes['summary-active-count'].textContent, '1');
    f.fireTimers();
    f.module.unmount();
    assert.equal(f.requests[2].signal.aborted, true);
    assert.equal(f.listeners.size, 0);
    f.module.mount();
    await f.respond(2, { page: 3, summary: { active_tasks: 99 } });
    assert.equal(f.nodes['summary-active-count'].textContent, '1');
    assert.equal(f.timers.size, 0);
    await f.respond(3, { summary: { active_tasks: 2 } });
    assert.equal(f.nodes['summary-active-count'].textContent, '2');
    assert.equal(f.timers.size, 1);
    f.module.unmount();
    assert.equal(f.timers.size, 0);
});

test('logs hidden visibility does not schedule over an in-flight request', async (t) => {
    const f = logsFixture(t);
    f.module.mount();
    f.doc.visibilityState = 'hidden';
    f.listeners.get('visibilitychange')();
    f.fireTimers();
    assert.equal(f.requests.length, 1);
    assert.equal(f.requests[0].signal.aborted, false);
    await f.respond(0);
    assert.equal([...f.timers.values()][0].delay, 30000);
});

test('logs failure retries use one backoff timer and preserve the current draft', async (t) => {
    const f = logsFixture(t);
    t.mock.method(console, 'error', () => {});
    f.module.mount();
    f.nodes['query-filter'].value = 'unfinished';
    await f.respond(0, {}, 503);
    assert.equal(f.timers.size, 1);
    assert.equal(f.nodes['query-filter'].value, 'unfinished');
    assert.match(f.nodes['logs-feedback'].textContent, /HTTP 503/);
    f.fireTimers();
    await f.respond(1, {}, 503);
    assert.equal([...f.timers.values()][0].delay, 4000);
    f.fireTimers();
    await f.respond(2);
    assert.equal(f.win.__telegraphLogsState.consecutiveFailures, 0);
    assert.equal(f.nodes['query-filter'].value, 'unfinished');
    assert.equal(f.timers.size, 1);
});
