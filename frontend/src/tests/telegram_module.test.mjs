import test from 'node:test';
import assert from 'node:assert/strict';
import { createDocument, walk } from './metadata_dom_fixture.mjs';
import { createTelegramModule } from '../telegram/index.js';
import { telegramMessages } from '../telegram/state.js';
const tick = () => new Promise(resolve => setImmediate(resolve));
const ok = payload => ({ response: { ok: true }, payload });
const snapshot = extra => ({ attempt_id: 'a'.repeat(32), seq: 1, revision: 1, state: 'waiting_qr', expires_at: new Date(Date.now() + 60000).toISOString(), ...extra });
function fixture(overrides = {}) {
    const doc = createDocument(); const create = doc.createElement;
    doc.createElement = tag => { const node = create(tag); node.append = (...nodes) => node.children.push(...nodes); node.removeAttribute = key => { delete node.attributes[key]; if (key === 'src') delete node.src; }; return node; };
    const root = doc.createElement('section'); doc.querySelector = selector => selector.includes('telegram-account') ? root : null;
    let count = 0; const intervals = new Map(); const timeouts = new Map(); const sources = [];
    const win = { setInterval: fn => { const id = ++count; intervals.set(id, fn); return id; }, clearInterval: id => intervals.delete(id), setTimeout: fn => { const id = ++count; timeouts.set(id, fn); return id; }, clearTimeout: id => timeouts.delete(id), EventSource: class { constructor(url) { this.url = url; sources.push(this); } close() { this.closed = true; } }, fetch: () => { throw new Error('unexpected network'); } };
    const api = { account: async () => ok({ state: 'auth_required', revision: 0 }), start: async () => ok(snapshot()), attempt: async () => ok(snapshot({ seq: 2, revision: 2 })), verify: async () => ok({}), cancel: async () => ok(snapshot({ state: 'cancelled', seq: 2, revision: 2 })), password: async () => ok({ accepted: true }), ...overrides };
    const module = createTelegramModule(win, doc, api); const byID = id => walk(root).find(node => node.getAttribute('id') === id);
    const submit = () => { const form = walk(root).find(node => node.tagName === 'FORM'); for (const fn of form.listeners.get('submit')) fn({ preventDefault() {} }); };
    return { module, root, byID, sources, intervals, timeouts, submit };
}
test('Telegram lifecycle releases EventSource timers QR and password', async () => {
    let signal;
    const f = fixture({ account: async options => { signal = options.signal; return ok({ state: 'auth_required', revision: 0 }); } });
    f.module.mount(); await tick(); f.byID('telegram-connect').emit('click'); await tick();
    const stream = f.sources[0]; stream.onmessage({ data: JSON.stringify(snapshot({ seq: 2, revision: 2, qr: 'data:image/png;base64,eA==', qr_expires_at: new Date(Date.now() + 30000).toISOString() })) });
    const image = walk(f.root).find(node => node.tagName === 'IMG'); const password = f.byID('telegram-password'); password.value = 'private-value'; assert.equal(image.hidden, false);
    f.module.unmount(); assert.equal(stream.closed, true); assert.equal(signal.aborted, true); assert.equal(f.intervals.size, 0); assert.equal(f.timeouts.size, 0); assert.equal(password.value, ''); assert.equal(image.src, undefined); assert.equal(f.root.children.length, 0);
    stream.onmessage({ data: JSON.stringify(snapshot({ state: 'connected', seq: 3, revision: 3 })) }); assert.equal(f.root.children.length, 0);
});
test('Telegram password clears before request and error survives timer renders', async () => {
    let passwordSent; let f;
    f = fixture({ start: async () => ok(snapshot({ state: 'password_required' })), password: async (_id, value) => { assert.equal(f.byID('telegram-password').value, ''); passwordSent = value; return { response: { ok: false }, payload: { code: 'password_invalid' } }; } });
    f.module.mount(); await tick(); f.byID('telegram-connect').emit('click'); await tick(); f.byID('telegram-password').value = 'private-value'; f.submit(); await tick();
    assert.equal(passwordSent, 'private-value'); for (const render of f.intervals.values()) render(); assert.equal(f.byID('telegram-attempt-status').textContent, telegramMessages.password_invalid); f.module.unmount();
});
test('Telegram stream reconnect fetches current snapshot and never replays QR', async () => {
    let fetched = 0; const f = fixture({ attempt: async () => { fetched++; return ok(snapshot({ seq: 4, revision: 4, state: 'password_required' })); } });
    f.module.mount(); await tick(); f.byID('telegram-connect').emit('click'); await tick(); f.sources[0].onerror(); assert.equal(f.sources[0].closed, true); assert.equal(f.timeouts.size, 1);
    const reconnect = [...f.timeouts.values()][0]; await reconnect(); await tick(); assert.equal(fetched, 1); assert.equal(f.sources.length, 2); assert.equal(f.timeouts.size, 0); assert.equal(f.byID('telegram-attempt-status').textContent, telegramMessages.password_required); f.module.unmount();
});
test('Telegram start failure remains visible without a snapshot', async () => {
    const f = fixture({ start: async () => ({ response: { ok: false }, payload: { code: 'busy' } }) }); f.module.mount(); await tick(); f.byID('telegram-connect').emit('click'); await tick(); for (const render of f.intervals.values()) render(); assert.equal(f.byID('telegram-attempt-status').textContent, telegramMessages.busy); f.module.unmount();
});
test('Telegram account displays the deployment source limit', async () => {
    const f = fixture({ account: async () => ok({ state: 'auth_required', revision: 0, max_source_bytes: 17 * 1024 * 1024 }) }); f.module.mount(); await tick(); assert.ok(walk(f.root).some(node => node.textContent.includes('17 MiB'))); f.module.unmount();
});
