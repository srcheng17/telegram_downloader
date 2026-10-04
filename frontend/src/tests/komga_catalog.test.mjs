import test from 'node:test';
import assert from 'node:assert/strict';
import { createKomgaModule } from '../komga/index.js';
import { createKomgaApi } from '../komga/api.js';
import { createDocument, walk } from './metadata_dom_fixture.mjs';

const ok = payload => ({ response: { ok: true, status: 200 }, payload });
const tick = () => new Promise(resolve => setImmediate(resolve));
const book = (id, extra = {}) => ({ id, library_id: 'lib1', title: `作品 ${id}`, series_title: '系列', file_name: `${id}.cbz`, file_type: '.cbz', editable: true, read_only_reason: '', ...extra });
const page = (number, books, totalPages = 2) => ({ books, page: number, size: 24, total_pages: totalPages, total_elements: 25 });

function fixture(apiOverrides = {}, options = {}) {
    const doc = createDocument();
    const root = doc.createElement('section'); root.id = 'komga-workspace';
    const form = doc.createElement('form'); root.appendChild(form);
    const controls = {};
    for (const [name, tag] of Object.entries({ library: 'select', search: 'input', list: 'ul', detail: 'section', feedback: 'p', prev: 'button', next: 'button' })) {
        const node = doc.createElement(tag); node.id = `komga-${name}`; (name === 'library' || name === 'search' ? form : root).appendChild(node); controls[name] = node;
    }
    controls.search.form = form;
    doc.querySelector = selector => selector === '#komga-workspace' ? root : null;
    let timerID = 0;
    const timers = new Map();
    const confirmations = [];
    const win = {
        setTimeout(fn) { const id = ++timerID; timers.set(id, fn); return id; },
        clearTimeout(id) { timers.delete(id); },
        confirm(message) { confirmations.push(message); return options.confirm ?? false; },
    };
    const calls = { libraries: 0, books: [], details: [] };
    const api = {
        async libraries() { calls.libraries++; return ok({ libraries: [{ id: 'lib1', name: '书库 1', unavailable: false, writable: true }, { id: 'lib2', name: '书库 2', unavailable: false, writable: false }] }); },
        async books(query, request) { calls.books.push({ query, signal: request.signal }); return ok(page(query.page, [book(`b${query.page + 1}`)])); },
        async book(id, request) { calls.details.push({ id, signal: request.signal }); return ok(book(id)); },
        ...apiOverrides,
    };
    const module = createKomgaModule(win, doc, api, { onBookDetail: null, ...options });
    const firstBookButton = () => walk(controls.list).find(node => node.tagName === 'BUTTON');
    const runTimers = () => { for (const [id, fn] of [...timers]) { timers.delete(id); fn(); } };
    return { module, root, form, controls, calls, win, timers, confirmations, firstBookButton, runTimers };
}

test('Komga catalog filters, pages, opens detail, and clears protected content on unmount', async () => {
    const f = fixture();
    f.module.mount(); await tick();
    assert.equal(f.controls.library.children.length, 2);
    assert.equal(f.calls.books.length, 1);
    assert.equal(f.calls.books[0].query.libraryID, 'lib1');
    assert.equal(f.firstBookButton().getAttribute('aria-pressed'), 'false');
    f.firstBookButton().emit('click'); await tick();
    assert.equal(f.controls.detail.children[0].textContent, '作品 b1');
    assert.equal(f.firstBookButton().getAttribute('aria-pressed'), 'true');
    f.controls.next.emit('click'); await tick();
    assert.equal(f.calls.books[1].query.page, 1);
    assert.equal(f.firstBookButton().children[0].textContent, '作品 b2');
    f.controls.search.value = '男同 漫画'; f.controls.search.emit('input'); f.runTimers(); await tick();
    assert.equal(f.calls.books[2].query.page, 0);
    assert.equal(f.calls.books[2].query.query, '男同 漫画');
    f.controls.search.value = '手动提交';
    let prevented = false;
    for (const listener of f.form.listeners.get('submit')) listener({ preventDefault() { prevented = true; } });
    await tick();
    assert.equal(prevented, true);
    assert.equal(f.calls.books[3].query.query, '手动提交');
    f.module.unmount();
    assert.equal(f.controls.list.children.length, 0);
    assert.equal(f.controls.detail.children.length, 0);
    assert.equal(f.controls.search.value, '');
    assert.equal(f.controls.feedback.textContent, '');
    assert.equal(f.controls.search.listeners.get('input')?.size, 0);
    assert.equal(f.form.listeners.get('submit')?.size, 0);
    assert.equal(f.timers.size, 0);
});

test('late list and detail responses cannot replace newer or unmounted content', async () => {
    let completeOld;
    let completeDetail;
    const f = fixture({
        books(query) {
            if (!query.query) return new Promise(resolve => { completeOld = resolve; });
            return Promise.resolve(ok(page(0, [book('new')], 1)));
        },
        book() { return new Promise(resolve => { completeDetail = resolve; }); },
    });
    f.module.mount(); await tick();
    f.controls.search.value = 'new'; f.controls.search.emit('input'); f.runTimers(); await tick();
    assert.equal(f.firstBookButton().children[0].textContent, '作品 new');
    completeOld(ok(page(0, [book('old')], 1))); await tick();
    assert.equal(f.firstBookButton().children[0].textContent, '作品 new');
    f.firstBookButton().emit('click'); await tick();
    f.module.unmount();
    completeDetail(ok(book('new'))); await tick();
    assert.equal(f.controls.detail.children.length, 0);
});

test('dirty editor survives catalog refresh and blocks accidental detail replacement', async () => {
    let unsaved = true;
    let disposed = 0;
    const f = fixture({
        books: async query => ok(page(query.page, [book('b1'), book('b2')], 1)),
    }, {
        onBookDetail: () => ({ hasUnsavedChanges: () => unsaved, dispose: () => { disposed++; } }),
    });
    f.module.mount(); await tick();
    f.firstBookButton().emit('click'); await tick();
    assert.equal(f.module.hasUnsavedChanges(), true);
    f.module.refresh(); await tick();
    assert.equal(f.controls.detail.children[0].textContent, '作品 b1');
    assert.equal(disposed, 0);
    const second = walk(f.controls.list).filter(node => node.tagName === 'BUTTON')[1];
    second.emit('click'); await tick();
    assert.equal(f.controls.detail.children[0].textContent, '作品 b1');
    assert.equal(f.confirmations.length, 1);
    unsaved = false;
    second.emit('click'); await tick();
    assert.equal(f.controls.detail.children[0].textContent, '作品 b2');
    assert.equal(disposed, 1);
    f.module.unmount();
});

test('failed next page retains the previous page and retry control', async () => {
    const f = fixture({
        books: async query => query.page === 0 ? ok(page(0, [book('first')])) : { response: { ok: false, status: 503 }, payload: { code: 'unavailable' } },
    });
    f.module.mount(); await tick();
    f.controls.next.emit('click'); await tick();
    assert.equal(f.firstBookButton().children[0].textContent, '作品 first');
    assert.equal(f.controls.next.disabled, false);
    assert.equal(f.controls.prev.disabled, true);
    assert.match(f.controls.feedback.textContent, /暂时不可用/);
    f.module.unmount();
});

test('removed library cannot erase a dirty editor during refresh', async () => {
    let libraries = [{ id: 'lib1', name: '书库 1', unavailable: false, writable: true }];
    let disposeCount = 0;
    const f = fixture({ libraries: async () => ok({ libraries }) }, {
        onBookDetail: () => ({ hasUnsavedChanges: () => true, dispose: () => { disposeCount++; } }),
    });
    f.module.mount(); await tick();
    f.firstBookButton().emit('click'); await tick();
    libraries = [];
    f.module.refresh(); await tick();
    assert.equal(f.controls.detail.children[0].textContent, '作品 b1');
    assert.equal(disposeCount, 0);
    assert.match(f.controls.feedback.textContent, /已保留未保存/);
    f.module.unmount();
});

test('editor integration receives a signal that ends with its page lifetime', async () => {
    let signal;
    const f = fixture({}, { onBookDetail: value => { signal = value.signal; return { dispose() {} }; } });
    f.module.mount(); await tick();
    f.firstBookButton().emit('click'); await tick();
    assert.equal(signal.aborted, false);
    f.module.unmount();
    assert.equal(signal.aborted, true);
});

test('editing after a detail refresh starts still protects the in-flight draft', async () => {
    let dirty = false;
    let completeRefresh;
    let details = 0;
    const f = fixture({
        book: async id => {
            details++;
            if (details === 1) return ok(book(id));
            return new Promise(resolve => { completeRefresh = resolve; });
        },
    }, { onBookDetail: () => ({ hasUnsavedChanges: () => dirty, dispose() {} }) });
    f.module.mount(); await tick();
    f.firstBookButton().emit('click'); await tick();
    const original = f.controls.detail.children[0];
    f.module.refresh(); await tick();
    dirty = true;
    completeRefresh(ok(book('b1', { title: '服务端新标题' }))); await tick();
    assert.equal(f.controls.detail.children[0], original);
    assert.match(f.controls.feedback.textContent, /草稿/);
    f.module.unmount();
});

test('read-only reason and authorization failure are shown in Chinese', async () => {
    const f = fixture({
        books: async query => ok(page(query.page, [book('pdf', { file_name: 'pdf.pdf', file_type: '.pdf', editable: false, read_only_reason: 'unsupported_format' })], 1)),
        book: async id => ok(book(id, { file_name: 'pdf.pdf', file_type: '.pdf', editable: false, read_only_reason: 'unsupported_format' })),
    });
    f.module.mount(); await tick();
    f.firstBookButton().emit('click'); await tick();
    assert.match(f.controls.detail.children[2].textContent, /不支持写回 ComicInfo/);
    f.module.unmount();
    const denied = fixture({ libraries: async () => ({ response: { ok: false, status: 403 }, payload: { code: 'forbidden' } }) });
    denied.module.mount(); await tick();
    assert.match(denied.controls.feedback.textContent, /没有读取书库的权限|没有访问权限/);
    denied.module.unmount();
});

test('unexpected adapter errors cannot expose upstream private text', async () => {
    const f = fixture({ book: async () => { throw new Error('private file:/hidden/secret.cbz'); } });
    f.module.mount(); await tick();
    f.firstBookButton().emit('click'); await tick();
    assert.match(f.controls.feedback.textContent, /详情读取失败/);
    assert.equal(f.controls.feedback.textContent.includes('file:/hidden'), false);
    f.module.unmount();
});

test('Komga API adapter encodes book search and keeps authenticated local transport', async () => {
    const calls = [];
    const win = {
        location: { origin: 'https://local.test' },
        fetch: async (url, options) => { calls.push({ url, options }); return { ok: true, status: 200, json: async () => ({ books: [] }) }; },
    };
    const api = createKomgaApi(win);
    await api.books({ libraryID: 'lib 1', query: 'A&B', page: 2, size: 24 });
    assert.equal(calls[0].url, '/api/komga/books?library_id=lib+1&query=A%26B&page=2&size=24');
    assert.equal(calls[0].options.credentials, 'same-origin');
    assert.equal(calls[0].options.cache, 'no-store');
    await api.book('id/one');
    assert.equal(calls[1].url, '/api/komga/books/id%2Fone');
});
