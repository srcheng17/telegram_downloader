import test from 'node:test';
import assert from 'node:assert/strict';
import { createCurrentTask } from '../home/current_task.js';

function fixture(api) {
    const timers = new Map(); let serial = 0; const changes = [];
    const win = { setTimeout(fn) { timers.set(++serial, fn); return serial; }, clearTimeout(id) { timers.delete(id); } };
    const doc = { visibilityState: 'visible', addEventListener() {}, removeEventListener() {} };
    const tracker = createCurrentTask({ api, win, doc, onChange: state => changes.push(state) });
    return { tracker, changes, timers, async tick() { const [id, fn] = timers.entries().next().value; timers.delete(id); await fn(); } };
}
const ok = payload => ({ response: { ok: true }, payload });
test('Komga delivery is not complete until a verified book ID is returned; pending retries do not recreate a task', async () => {
    let copies = 0; const calls = [];
    const f = fixture({ async getJson() { return ok({ task: { id: 'one', status: 'SUCCEEDED', available_actions: ['download', 'copy_to_komga'] } }); }, async postJson(url) { calls.push(url); copies += 1; return ok(copies === 1 ? { ok: true, komga_indexed: 'pending' } : { ok: true, komga_indexed: 'verified', book_id: 'book-one', library_id: 'library-one' }); } });
    await f.tracker.start('one', 'komga'); assert.equal(f.changes.at(-1).delivery.status, 'pending');
    await f.tick(); assert.equal(f.changes.at(-1).delivery.status, 'indexed'); assert.equal(f.timers.size, 0);
    assert.deepEqual(calls, ['/api/tasks/one/copy-to-komga', '/api/tasks/one/copy-to-komga']);
    f.tracker.stop();
});
test('late task response after teardown cannot render or schedule another poll', async () => {
    let resolve; const f = fixture({ getJson: () => new Promise(done => { resolve = done; }) });
    const waiting = f.tracker.start('one', 'download'); f.tracker.stop(); const rendered = f.changes.length;
    resolve(ok({ task: { id: 'one', status: 'RUNNING', available_actions: ['cancel'] } })); await waiting;
    assert.equal(f.changes.length, rendered); assert.equal(f.timers.size, 0);
});
test('unverified or unavailable Komga delivery preserves the artifact and offers delivery-only retry', async () => {
    const f = fixture({ async getJson() { return ok({ task: { id: 'one', status: 'SUCCEEDED', available_actions: ['download', 'copy_to_komga'] } }); }, async postJson() { throw new Error('private upstream'); } });
    await f.tracker.start('one', 'komga'); const state = f.changes.at(-1);
    assert.equal(state.delivery.status, 'failed'); assert.equal(state.task.id, 'one'); assert.doesNotMatch(state.error, /private upstream/); assert.equal(f.timers.size, 0);
});

test('verified delivery requires bounded valid identities for both the library and book', async () => {
    for (const key of ['book_id', 'library_id']) for (const invalid of [undefined, '', ' ', 123, true, {}, '../book', 'book\nother', 'x'.repeat(129)]) {
        const payload = { ok: true, komga_indexed: 'verified', book_id: 'book-one', library_id: 'library-one', [key]: invalid };
        const f = fixture({ async getJson() { return ok({ task: { id: 'one', status: 'SUCCEEDED', available_actions: ['download', 'copy_to_komga'] } }); }, async postJson() { return ok(payload); } });
        await f.tracker.start('one', 'komga');
        assert.equal(f.changes.at(-1).delivery.status, 'pending', `${key}=${String(invalid)}`);
        f.tracker.stop();
    }
});
