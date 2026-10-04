// Owns only tab selection/ARIA. Page modules own lazy mounting and cleanup.
export function createTabs({ root, win = window, defaultTab, hash = false, onChange = () => {} }) {
    let mounted = false;
    let active = null;
    let tabs = [];
    let panels = [];
    const removers = [];
    function listen(node, type, listener) { node.addEventListener(type, listener); removers.push(() => node.removeEventListener(type, listener)); }
    function fromHash() {
        try { return decodeURIComponent(win.location.hash.slice(1)); } catch { return ''; }
    }
    function activate(id, { focus = false, updateHash = true } = {}) {
        if (!mounted || !tabs.some(tab => tab.dataset.tab === id)) return false;
        const previous = active;
        active = id;
        for (const tab of tabs) {
            const selected = tab.dataset.tab === id;
            tab.setAttribute('aria-selected', String(selected));
            tab.tabIndex = selected ? 0 : -1;
            if (selected && focus) tab.focus();
        }
        for (const panel of panels) panel.hidden = panel.dataset.tabPanel !== id;
        if (hash && updateHash && fromHash() !== id) win.history.replaceState(win.history.state, '', `${win.location.pathname}${win.location.search}#${encodeURIComponent(id)}`);
        if (previous !== id) onChange(id, previous);
        return true;
    }
    function mount() {
        if (mounted || !root) return;
        tabs = [...root.querySelectorAll('[data-tab]')];
        panels = [...root.querySelectorAll('[data-tab-panel]')];
        if (!tabs.length) return;
        mounted = true;
        for (const [index, tab] of tabs.entries()) {
            const id = tab.dataset.tab;
            const panel = panels.find(panel => panel.dataset.tabPanel === id);
            if (!panel) continue;
            tab.id ||= `${root.id || 'tabs'}-tab-${id}`;
            panel.id ||= `${root.id || 'tabs'}-panel-${id}`;
            tab.setAttribute('role', 'tab'); tab.setAttribute('aria-controls', panel.id);
            panel.setAttribute('role', 'tabpanel'); panel.setAttribute('aria-labelledby', tab.id);
            listen(tab, 'click', () => activate(id));
            listen(tab, 'keydown', event => {
                const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : event.key === 'ArrowRight' ? (index + 1) % tabs.length : event.key === 'ArrowLeft' ? (index + tabs.length - 1) % tabs.length : -1;
                if (next < 0) return;
                event.preventDefault(); activate(tabs[next].dataset.tab, { focus: true });
            });
        }
        if (hash) listen(win, 'hashchange', () => activate(fromHash(), { updateHash: false }));
        const initial = hash && tabs.some(tab => tab.dataset.tab === fromHash()) ? fromHash() : defaultTab || tabs[0].dataset.tab;
        activate(initial, { updateHash: false });
    }
    function unmount() { mounted = false; active = null; removers.splice(0).forEach(remove => remove()); tabs = []; panels = []; }
    return { mount, unmount, activate, getActive: () => active };
}
