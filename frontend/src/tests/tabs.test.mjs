import test from 'node:test';
import assert from 'node:assert/strict';
import { createTabs } from '../shared/tabs.js';

function fixture(hash = '#ai') {
    const make = (name, key) => ({ dataset: { [name]: key }, attrs: {}, listeners: new Map(), hidden: false,
        setAttribute(name, value) { this.attrs[name] = String(value); }, getAttribute(name) { return this.attrs[name]; },
        addEventListener(name, fn) { this.listeners.set(name, fn); }, removeEventListener(name) { this.listeners.delete(name); },
        focus() { this.focused = true; }, emit(name, extra = {}) { this.listeners.get(name)?.({ preventDefault() {}, ...extra }); },
    });
    const buttons = ['download', 'ai', 'connections'].map(id => make('tab', id));
    const panels = ['download', 'ai', 'connections'].map(id => make('tabPanel', id));
    const root = { id: 'settings', querySelectorAll: selector => selector === '[data-tab]' ? buttons : panels };
    const listeners = new Map();
    const win = { location: { pathname: '/settings', search: '', hash }, history: { state: { retained: true }, replaceState(state, _title, url) { this.state = state; win.location.hash = url.slice(url.indexOf('#')); } }, addEventListener: (name, fn) => listeners.set(name, fn), removeEventListener: name => listeners.delete(name) };
    const changes = [];
    const tabs = createTabs({ root, win, defaultTab: 'download', hash: true, onChange: (...args) => changes.push(args) });
    return { buttons, panels, win, listeners, changes, tabs };
}

test('tabs honor deep links, expose one selected panel, and keep repeated mounts idempotent', () => {
    const f = fixture(); f.tabs.mount(); f.tabs.mount();
    assert.equal(f.tabs.getActive(), 'ai'); assert.equal(f.changes.length, 1);
    assert.deepEqual(f.panels.map(panel => panel.hidden), [true, false, true]);
    assert.equal(f.buttons[1].attrs.role, 'tab'); assert.equal(f.buttons[1].attrs['aria-selected'], 'true');
    assert.equal(f.buttons[1].attrs['aria-controls'], f.panels[1].id);
    assert.equal(f.panels[1].attrs['aria-labelledby'], f.buttons[1].id);
    f.buttons[2].emit('click'); assert.equal(f.win.location.hash, '#connections');
    assert.deepEqual(f.win.history.state, { retained: true });
    f.tabs.unmount(); f.buttons[0].emit('click'); assert.equal(f.changes.length, 2);
    assert.equal(f.listeners.size, 0);
});

test('tabs support arrows Home End, safe unknown hashes and browser hash navigation', () => {
    const f = fixture('#unknown'); f.tabs.mount(); assert.equal(f.tabs.getActive(), 'download');
    f.buttons[0].emit('keydown', { key: 'ArrowLeft' }); assert.equal(f.tabs.getActive(), 'connections'); assert.equal(f.buttons[2].focused, true);
    f.buttons[2].emit('keydown', { key: 'Home' }); assert.equal(f.tabs.getActive(), 'download');
    f.buttons[0].emit('keydown', { key: 'End' }); assert.equal(f.tabs.getActive(), 'connections');
    f.buttons[2].emit('keydown', { key: 'ArrowRight' }); assert.equal(f.tabs.getActive(), 'download');
    f.win.location.hash = '#ai'; f.listeners.get('hashchange')(); assert.equal(f.tabs.getActive(), 'ai');
    assert.equal(f.tabs.activate('missing'), false); f.tabs.unmount();
});
