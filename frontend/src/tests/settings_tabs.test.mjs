import test from 'node:test';
import assert from 'node:assert/strict';
globalThis.window = {}; globalThis.document = {};
const { createSettingsModule } = await import('../settings.js');
delete globalThis.window; delete globalThis.document;
const tick = () => new Promise(resolve => setImmediate(resolve));
function fixture(hash) {
    const element = dataset => ({ dataset, listeners: new Map(), attrs: {}, value: '', disabled: false, classList: { add() {}, remove() {} },
        setAttribute(name, value) { this.attrs[name] = String(value); }, getAttribute(name) { return this.attrs[name]; },
        addEventListener(name, fn) { this.listeners.set(name, fn); }, removeEventListener(name) { this.listeners.delete(name); },
        emit(name) { return this.listeners.get(name)?.({ currentTarget: this, preventDefault() {} }); }, focus() {},
    });
    const ids = ['download', 'sources', 'ai', 'fields', 'rules', 'connections', 'security'];
    const buttons = ids.map(id => element({ tab: id })); const panels = ids.map(id => element({ tabPanel: id }));
    const page = { id: 'settings-page', querySelectorAll: selector => selector === '[data-tab]' ? buttons : panels };
    const inputs = { '#timeout': element(), '#retries': element(), '#image_concurrency': element(), 'button[type="submit"]': element() }; inputs['#timeout'].value = '30';
    const radio = { checked: true, value: 'browser' }; const form = element();
    form.querySelector = selector => selector.includes(':checked') ? radio : inputs[selector]; form.querySelectorAll = () => [radio];
    const feedback = element(); const doc = { querySelector: selector => selector === 'form.settings-form' ? form : null, getElementById: id => id === 'settings-page' ? page : id === 'settings-feedback' ? feedback : null };
    const events = new Map(); const reads = []; const calls = []; const instances = new Map();
    const win = { location: { pathname: '/settings', search: '', hash }, history: { state: null, replaceState(_state, _title, url) { win.location.hash = url.slice(url.indexOf('#')); } }, addEventListener: (event, fn) => events.set(event, fn), removeEventListener: event => events.delete(event), fetch: async url => { reads.push(url); return new Response(JSON.stringify({ timeout: 30, retries: 3, image_concurrency: 2, download_action_mode: 'browser' }), { status: 200 }); } };
    const factories = Object.fromEntries(ids.filter(id => id !== 'download').map(id => [id, () => {
        calls.push(`create:${id}`);
        const instance = { text: '', mount() { calls.push(`mount:${id}`); }, unmount() { calls.push(`unmount:${id}`); }, pause() { calls.push(`pause:${id}`); }, resume() { calls.push(`resume:${id}`); } };
        instances.set(id, instance); return instance;
    }]));
    const module = createSettingsModule(win, doc, factories);
    return { module, inputs, form, feedback, reads, calls, instances, win, events, panels, click: id => buttons[ids.indexOf(id)].emit('click') };
}

test('settings mounts only the deep-linked tab, retains ordinary drafts and hydrates download on first visit', async () => {
    const f = fixture('#ai'); f.module.mount(); f.module.mount(); await tick();
    assert.deepEqual(f.calls, ['create:ai', 'mount:ai']); assert.deepEqual(f.reads, []);
    f.instances.get('ai').text = 'unsaved model';
    f.click('download'); await tick(); assert.equal(f.reads.length, 1);
    f.inputs['#timeout'].value = '45'; f.form.emit('input');
    f.click('sources'); f.click('ai');
    assert.equal(f.instances.get('ai').text, 'unsaved model'); assert.equal(f.calls.filter(call => call === 'create:ai').length, 1);
    f.click('download'); await tick(); assert.equal(f.inputs['#timeout'].value, '45'); assert.equal(f.reads.length, 1);
    assert.ok(f.calls.includes('pause:ai')); assert.ok(f.calls.includes('resume:ai'));
    assert.equal(f.calls.some(call => /fields|rules|security|connections/.test(call)), false);
    f.module.unmount(); assert.equal(f.events.size, 0);
});

test('settings exclusively mounts connections while selected and remounts on return', () => {
    const f = fixture('#connections'); f.module.mount();
    assert.deepEqual(f.calls, ['create:connections', 'mount:connections']);
    f.click('security'); assert.ok(f.calls.includes('unmount:connections'));
    f.click('connections'); assert.equal(f.calls.filter(call => call === 'mount:connections').length, 2);
    f.module.unmount(); assert.equal(f.calls.filter(call => call === 'unmount:connections').length, 2);
    assert.equal(f.panels.filter(panel => !panel.hidden).length, 1);
});

test('download hydration failure is visible and returning retries until success without overwriting later edits', async () => {
    for (const failure of ['http', 'network']) {
        const f = fixture('#download'); let attempts = 0;
        f.win.fetch = async () => {
            attempts++;
            if (attempts === 1) {
                if (failure === 'network') throw new Error('synthetic network failure');
                return new Response(JSON.stringify({ code: 'unavailable' }), { status: 503 });
            }
            return new Response(JSON.stringify({ timeout: 90, retries: 3, image_concurrency: 2, download_action_mode: 'browser' }), { status: 200 });
        };
        f.module.mount(); await tick();
        assert.match(f.feedback.textContent, /读取.*失败/); assert.equal(f.inputs['#timeout'].value, '30');
        f.click('ai'); f.click('download'); await tick();
        assert.equal(attempts, 2); assert.equal(f.inputs['#timeout'].value, '90');
        f.inputs['#timeout'].value = '120'; f.form.emit('input'); f.click('sources'); f.click('download'); await tick();
        assert.equal(attempts, 2); assert.equal(f.inputs['#timeout'].value, '120'); f.module.unmount();
    }
});

test('returning during a download read deduplicates it and edits cancel incomplete hydration safely', async () => {
    const f = fixture('#download'); let complete; let attempts = 0; let signal;
    f.win.fetch = (_url, options) => { attempts++; signal = options.signal; return new Promise(resolve => { complete = resolve; }); };
    f.module.mount(); f.click('ai'); f.click('download'); assert.equal(attempts, 1);
    f.inputs['#timeout'].value = '60'; f.form.emit('input'); assert.equal(signal.aborted, true);
    assert.match(f.feedback.textContent, /保留当前编辑/);
    complete(new Response(JSON.stringify({ timeout: 90 }), { status: 200 })); await tick();
    f.click('ai'); f.click('download'); await tick();
    assert.equal(attempts, 1); assert.equal(f.inputs['#timeout'].value, '60'); f.module.unmount();
});
