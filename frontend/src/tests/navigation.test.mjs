import test from 'node:test';
import assert from 'node:assert/strict';
import { createNavigationDisclosure } from '../shared/navigation.js';
import { createAppLifecycle } from '../shared/app_lifecycle.js';
import { navigationFixture } from './navigation_dom_fixture.mjs';
function setup(options) { const f = navigationFixture(options); f.navigation = createNavigationDisclosure({ root: f.doc, doc: f.doc, win: f.win }); f.navigation.mount(); return f; }

test('desktop hide preserves only the sidebar preference and removes hidden links from focus', () => {
    const f = setup({ collapsed: true });
    assert.equal(f.layout.classList.contains('is-sidebar-collapsed'), true);
    assert.equal(f.desktopToggle.getAttribute('aria-expanded'), 'false');
    assert.equal(f.desktopToggle.getAttribute('aria-label'), '显示侧栏');
    assert.equal(f.sidebar.inert, true);
    assert.equal(f.sidebar.getAttribute('aria-hidden'), 'true');
    assert.equal(f.link.getAttribute('aria-label'), '新建任务');
    assert.equal(f.link.getAttribute('title'), '新建任务');
    f.navigation.close(); assert.equal(f.layout.classList.contains('is-sidebar-collapsed'), true);
    f.desktopToggle.emit('click'); assert.equal(f.layout.classList.contains('is-sidebar-collapsed'), false);
    assert.equal(f.sidebar.inert, false);
    assert.equal(f.sidebar.getAttribute('aria-hidden'), null);
    assert.deepEqual(f.writes, [['workspace.sidebar.collapsed', 'false']]);
    f.navigation.unmount();
});
test('mobile drawer traps focus, isolates background, closes with Escape and returns focus', () => {
    const f = setup({ mobile: true });
    assert.equal(f.sidebar.inert, true); f.mobileToggle.focus(); f.mobileToggle.emit('click');
    assert.equal(f.sidebar.inert, false); assert.equal(f.sidebar.getAttribute('role'), 'dialog'); assert.equal(f.sidebar.getAttribute('aria-modal'), 'true');
    assert.equal(f.main.inert, true); assert.equal(f.skip.inert, true); assert.equal(f.backdrop.hidden, false); assert.equal(f.body.classList.contains('is-navigation-open'), true);
    assert.equal(f.doc.activeElement, f.closeButton);
    f.lastLink.focus(); const tab = f.doc.emit('keydown', { key: 'Tab' }); assert.equal(tab.defaultPrevented, true); assert.equal(f.doc.activeElement, f.closeButton);
    const shiftTab = f.doc.emit('keydown', { key: 'Tab', shiftKey: true }); assert.equal(shiftTab.defaultPrevented, true); assert.equal(f.doc.activeElement, f.lastLink);
    f.input.focus(); assert.equal(f.doc.activeElement, f.closeButton);
    f.doc.emit('keydown', { key: 'Escape' });
    assert.equal(f.main.inert, false); assert.equal(f.skip.inert, false); assert.equal(f.sidebar.inert, true); assert.equal(f.backdrop.hidden, true); assert.equal(f.doc.activeElement, f.mobileToggle);
    assert.equal(f.body.classList.contains('is-navigation-open'), false); assert.deepEqual(f.writes, []); f.navigation.unmount();
});
test('mobile backdrop and close button restore previous inert state; resize preserves desktop preference', () => {
    const f = setup({ collapsed: true });
    f.resize(true); f.main.inert = true; f.mobileToggle.emit('click'); f.backdrop.emit('click'); assert.equal(f.main.inert, true); f.main.inert = false;
    f.mobileToggle.emit('click'); f.closeButton.emit('click'); assert.equal(f.mobileToggle.getAttribute('aria-expanded'), 'false');
    f.mobileToggle.emit('click'); f.resize(false);
    assert.equal(f.layout.classList.contains('is-sidebar-collapsed'), true); assert.equal(f.sidebar.inert, true); assert.equal(f.sidebar.getAttribute('aria-modal'), null); assert.equal(f.backdrop.hidden, true); assert.equal(f.main.inert, false);
    assert.equal(f.doc.activeElement, f.desktopToggle); assert.deepEqual(f.writes, []);
    f.resize(true); assert.equal(f.sidebar.inert, true); assert.equal(f.mobileToggle.getAttribute('aria-expanded'), 'false'); f.navigation.unmount();
});
test('navigation repeated mount and disposal leave no event or inert state behind', () => {
    const f = setup({ mobile: true, storageBlocked: true }); f.navigation.mount(); assert.equal(f.mobileToggle.listeners.get('click').size, 1);
    f.mobileToggle.emit('click'); f.navigation.unmount(); assert.equal(f.main.inert, false); assert.equal(f.skip.inert, false); assert.equal(f.backdrop.hidden, true);
    assert.equal(f.doc.listeners.get('keydown').size, 0); assert.equal(f.doc.listeners.get('focusin').size, 0); assert.equal(f.media.listeners.get('change').size, 0);
    f.navigation.mount(); f.resize(false); f.desktopToggle.emit('click'); assert.equal(f.layout.classList.contains('is-sidebar-collapsed'), true); f.navigation.unmount();
});
test('htmx swaps close mobile navigation while desktop collapse survives lifecycle mounts', async () => {
    const f = navigationFixture({ collapsed: true });
    const session = { mount() {}, clear() {}, refresh: async () => ({ authenticated: true }) };
    const app = createAppLifecycle(f.win, f.doc, session); await app.start();
    await app.afterSwap({ detail: { target: { id: 'content' } } }); assert.equal(f.layout.classList.contains('is-sidebar-collapsed'), true);
    f.resize(true); f.mobileToggle.emit('click');
    app.beforeSwap({ detail: { target: { id: 'content' }, shouldSwap: false } }); assert.equal(f.mobileToggle.getAttribute('aria-expanded'), 'true');
    app.beforeSwap({ detail: { target: { id: 'content' }, shouldSwap: true } }); assert.equal(f.mobileToggle.getAttribute('aria-expanded'), 'false'); assert.equal(f.main.inert, false);
    f.mobileToggle.emit('click'); app.pagehide(); assert.equal(f.mobileToggle.getAttribute('aria-expanded'), 'false'); assert.equal(f.main.inert, false);
    f.resize(false); await app.restore(); assert.equal(f.layout.classList.contains('is-sidebar-collapsed'), true);
});
test('empty mobile drawer remains keyboard dismissible and authorization cleanup restores background', async () => {
    const f = navigationFixture({ mobile: true });
    const session = { mount() {}, clear() {}, refresh: async () => ({ authenticated: true }) };
    const app = createAppLifecycle(f.win, f.doc, session); await app.start();
    f.sidebar.querySelectorAll = () => [];
    f.mobileToggle.emit('click'); assert.equal(f.doc.activeElement, f.sidebar);
    const tab = f.doc.emit('keydown', { key: 'Tab' }); assert.equal(tab.defaultPrevented, true); assert.equal(f.doc.activeElement, f.sidebar);
    app.unauthorized(); assert.equal(f.main.inert, false); assert.equal(f.backdrop.hidden, true); assert.equal(f.doc.activeElement, f.mobileToggle);
});
