import test from 'node:test';
import assert from 'node:assert/strict';
import { createKomgaConnectionModule } from '../settings/komga_connection.js';

const tick = () => new Promise(resolve => setImmediate(resolve));
const ok = payload => ({ response: { ok: true }, payload });

function fixture(api) {
    function control(value = '') {
        const listeners = new Map();
        return {
            value, textContent: '', hidden: false, disabled: false,
            addEventListener(type, callback) { listeners.set(type, callback); },
            removeEventListener(type) { listeners.delete(type); },
            emit(type) { return listeners.get(type)?.({ currentTarget: this, preventDefault() {} }); },
            focus() { this.focused = true; },
        };
    }
    const controls = Object.fromEntries([
        'komga-connection', 'komga-connection-form', 'komga-base-url',
        'komga-credential-action', 'komga-new-key-row', 'komga-new-key',
        'komga-credential-state', 'komga-save', 'komga-test',
        'komga-reload', 'komga-connection-feedback',
    ].map(id => [id, control()]));
    controls['komga-credential-action'].value = 'keep';
    const doc = { getElementById: id => controls[id], createElement: () => control() };
    const win = { confirm: () => true };
    const module = createKomgaConnectionModule(win, doc, api);
    return { module, controls, el: id => controls[`komga-${id}`] };
}

test('Komga connection saves with expected version, clears secret and tests only saved settings', async () => {
    const writes = [];
    const tests = [];
    const f = fixture({
        get: async () => ok({ base_url: 'https://old.example', credential_configured: true, config_version: 4 }),
        save: async input => { writes.push(input); return ok({ base_url: input.base_url, credential_configured: true, config_version: 5 }); },
        test: async version => { tests.push(version); return ok({ status: 'connected', allowed_library_count: 2 }); },
    });
    f.module.mount(); await tick();
    assert.equal(f.el('base-url').value, 'https://old.example');
    f.el('base-url').value = 'https://new.example'; f.el('base-url').emit('change');
    await f.el('test').emit('click');
    assert.deepEqual(tests, []);
    f.el('credential-action').value = 'replace'; f.el('credential-action').emit('change');
    f.el('new-key').value = 'synthetic-secret';
    await f.el('connection-form').emit('submit');
    assert.deepEqual(writes, [{ expected_version: 4, base_url: 'https://new.example', credential: { action: 'replace', value: 'synthetic-secret' } }]);
    assert.equal(f.el('new-key').value, '');
    assert.equal(f.el('credential-action').value, 'keep');
    assert.doesNotMatch(f.el('connection-feedback').textContent, /synthetic-secret/);
    await f.el('test').emit('click');
    assert.deepEqual(tests, [5]);
    assert.match(f.el('connection-feedback').textContent, /2 个书库/);
    f.module.unmount();
});

test('Komga connection ignores stale read and clears password on unmount', async () => {
    let finish;
    const f = fixture({
        get: () => new Promise(resolve => { finish = resolve; }),
        save: async () => { throw new Error('unexpected save'); },
        test: async () => { throw new Error('unexpected test'); },
    });
    f.module.mount();
    f.el('base-url').value = 'https://draft.example';
    f.el('base-url').emit('change');
    finish(ok({ base_url: 'https://stale.example', credential_configured: false, config_version: 1 }));
    await tick();
    assert.equal(f.el('base-url').value, 'https://draft.example');
    f.el('new-key').value = 'synthetic-secret';
    f.module.unmount();
    assert.equal(f.el('new-key').value, '');
});
