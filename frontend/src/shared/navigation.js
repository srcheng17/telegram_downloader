const SIDEBAR_PREFERENCE = 'workspace.sidebar.collapsed';
const MOBILE_QUERY = '(max-width: 767px)';
const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

// This controller owns the persistent shell, independently of htmx page modules.
// Only desktop collapse is a preference; a mobile drawer always starts closed.
export function createNavigationDisclosure({ root, doc = globalThis.document, win = globalThis.window }) {
    const sidebar = root?.querySelector('.workspace-sidebar');
    const layout = root?.querySelector('.workspace-layout');
    const desktopToggle = root?.querySelector('[data-sidebar-toggle]');
    const mobileToggles = Array.from(root?.querySelectorAll?.('[data-navigation-toggle]') || []);
    const closeButtons = Array.from(root?.querySelectorAll?.('[data-navigation-close]') || []);
    const backdrop = root?.querySelector('[data-navigation-backdrop]');
    const media = win?.matchMedia?.(MOBILE_QUERY);
    const original = new Map(['role', 'aria-modal', 'aria-hidden', 'aria-label', 'tabindex'].map(name => [name, sidebar?.getAttribute(name)]));
    const originallyInert = Boolean(sidebar?.inert);
    const background = new Map();
    let mounted = false;
    let collapsed = false;
    let mobile = Boolean(media?.matches);
    let mobileOpen = false;
    let returnFocus = null;

    function restoreAttribute(name) {
        const value = original.get(name);
        if (value == null) sidebar.removeAttribute(name);
        else sidebar.setAttribute(name, value);
    }
    function render() {
        if (!sidebar || !layout) return;
        layout.classList.toggle('is-sidebar-collapsed', !mobile && collapsed);
        layout.classList.toggle('is-navigation-open', mobileOpen);
        sidebar.classList.toggle('is-open', mobileOpen);
        doc.body?.classList?.toggle('is-navigation-open', mobileOpen);
        if (backdrop) backdrop.hidden = !mobileOpen;
        sidebar.inert = mobile ? !mobileOpen : collapsed;
        if (sidebar.inert) sidebar.setAttribute('aria-hidden', 'true');
        else restoreAttribute('aria-hidden');
        if (mobileOpen) {
            sidebar.setAttribute('role', 'dialog');
            sidebar.setAttribute('aria-modal', 'true');
            sidebar.setAttribute('aria-label', '主导航');
            sidebar.setAttribute('tabindex', '-1');
        } else {
            for (const name of ['role', 'aria-modal', 'aria-label', 'tabindex']) restoreAttribute(name);
        }
        if (desktopToggle) {
            desktopToggle.setAttribute('aria-controls', sidebar.id);
            desktopToggle.setAttribute('aria-expanded', String(!collapsed));
            desktopToggle.setAttribute('aria-label', collapsed ? '显示侧栏' : '隐藏侧栏');
            desktopToggle.setAttribute('title', collapsed ? '显示侧栏' : '隐藏侧栏');
        }
        for (const button of mobileToggles) {
            button.setAttribute('aria-controls', sidebar.id);
            button.setAttribute('aria-expanded', String(mobileOpen));
            button.setAttribute('aria-label', mobileOpen ? '关闭导航' : '打开导航');
        }
    }
    function isolateBackground() {
        // Walk out from the dialog to cover the main area, toolbar and skip link,
        // regardless of which wrapper the server template uses for the toolbar.
        let branch = sidebar;
        while (branch?.parentElement) {
            const parent = branch.parentElement;
            for (const sibling of parent.children) {
                if (sibling === branch || sibling === backdrop || backdrop && sibling.contains(backdrop)) continue;
                background.set(sibling, Boolean(sibling.inert));
                sibling.inert = true;
            }
            if (parent === doc.body) break;
            branch = parent;
        }
    }
    function restoreBackground() {
        for (const [element, inert] of background) element.inert = inert;
        background.clear();
    }
    function focusable() {
        return Array.from(sidebar.querySelectorAll(FOCUSABLE)).filter(element => !element.disabled && !element.hidden
            && element.getAttribute('tabindex') !== '-1'
            && !element.closest('[hidden], [inert]') && element.getAttribute('aria-hidden') !== 'true'
            && (typeof element.getClientRects !== 'function' || element.getClientRects().length > 0));
    }
    function focusFirst() { (focusable()[0] || sidebar).focus(); }
    function close({ restoreFocus = true } = {}) {
        if (!mobileOpen) return;
        mobileOpen = false;
        restoreBackground();
        if (restoreFocus) {
            const target = returnFocus?.isConnected !== false ? returnFocus : mobileToggles[0];
            target?.focus();
        }
        returnFocus = null;
        render();
    }
    function toggleMobile(event) {
        if (!mounted || !mobile) return;
        if (mobileOpen) { close(); return; }
        returnFocus = event.currentTarget || doc.activeElement;
        mobileOpen = true;
        render();
        focusFirst();
        isolateBackground();
    }
    function toggleDesktop() {
        if (!mounted || mobile) return;
        collapsed = !collapsed;
        try { win.localStorage?.setItem(SIDEBAR_PREFERENCE, String(collapsed)); } catch { /* Storage can be blocked; in-memory navigation still works. */ }
        render();
    }
    function keydown(event) {
        if (!mobileOpen) return;
        if (event.key === 'Escape') {
            event.preventDefault();
            event.stopPropagation();
            close();
        } else if (event.key === 'Tab') {
            const elements = focusable();
            const first = elements[0];
            const last = elements.at(-1);
            if (!first) { event.preventDefault(); sidebar.focus(); return; }
            if (!sidebar.contains(doc.activeElement) || doc.activeElement === sidebar || !event.shiftKey && doc.activeElement === last) {
                event.preventDefault(); first.focus();
            } else if (event.shiftKey && doc.activeElement === first) {
                event.preventDefault(); last.focus();
            }
        }
    }
    function focusin(event) { if (mobileOpen && !sidebar.contains(event.target)) focusFirst(); }
    function viewportChanged() {
        const next = Boolean(media?.matches);
        if (mobile === next) return;
        const wasOpen = mobileOpen;
        if (next && sidebar.contains(doc.activeElement)) mobileToggles[0]?.focus();
        mobile = next;
        if (wasOpen) close({ restoreFocus: false });
        else render();
        if (wasOpen && !mobile) desktopToggle?.focus();
    }
    function closeFromControl() { close(); }
    return {
        mount() {
            if (mounted || !sidebar || !layout) return;
            mounted = true;
            if (!sidebar.id) sidebar.id = 'workspace-sidebar';
            try { collapsed = win.localStorage?.getItem(SIDEBAR_PREFERENCE) === 'true'; } catch { /* A blocked preference is not a navigation failure. */ }
            mobile = Boolean(media?.matches);
            desktopToggle?.addEventListener('click', toggleDesktop);
            for (const button of mobileToggles) button.addEventListener('click', toggleMobile);
            for (const button of closeButtons) button.addEventListener('click', closeFromControl);
            backdrop?.addEventListener('click', closeFromControl);
            doc.addEventListener('keydown', keydown);
            doc.addEventListener('focusin', focusin);
            media?.addEventListener('change', viewportChanged);
            for (const link of sidebar.querySelectorAll('.nav-link')) {
                const label = link.getAttribute('aria-label') || link.textContent.trim();
                if (label) { link.setAttribute('aria-label', label); link.setAttribute('title', label); }
            }
            render();
        },
        unmount() {
            if (!mounted) return;
            close();
            mounted = false;
            desktopToggle?.removeEventListener('click', toggleDesktop);
            for (const button of mobileToggles) button.removeEventListener('click', toggleMobile);
            for (const button of closeButtons) button.removeEventListener('click', closeFromControl);
            backdrop?.removeEventListener('click', closeFromControl);
            doc.removeEventListener('keydown', keydown);
            doc.removeEventListener('focusin', focusin);
            media?.removeEventListener('change', viewportChanged);
            restoreBackground();
            for (const name of original.keys()) restoreAttribute(name);
            sidebar.inert = originallyInert;
        },
        close,
    };
}
