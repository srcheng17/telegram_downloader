import { mountPageModules, syncActiveNav, unmountPageModules } from './page_modules.js';
import { getAdminSession } from './admin_session.js';
import { createNavigationDisclosure } from './navigation.js';

export function createAppLifecycle(win, doc, session = getAdminSession(win, doc)) {
    let generation = 0;
    let redirecting = false;
    const navigation = createNavigationDisclosure({ root: doc, doc, win });
    const content = () => doc.getElementById('content');
    function unauthorized() {
        navigation.close();
        generation += 1;
        unmountPageModules(win);
        session.clear();
        content()?.replaceChildren();
        if (!redirecting && win.location.pathname !== '/auth/login') {
            redirecting = true;
            win.location.replace('/auth/login');
        }
        return true;
    }
    win.__onAdminUnauthorized = unauthorized;
    async function mount({ revalidate = false } = {}) {
        const version = ++generation;
        navigation.close();
        const root = content();
        if (win.location.pathname === '/auth/login') {
            // pagehide clears passwords/session state; BFCache keeps the form listeners.
            if (root) root.hidden = false;
            return;
        }
        if (revalidate && root) root.hidden = true;
        try {
            const result = await session.refresh();
            if (version !== generation) return;
            if (!result.authenticated) { unauthorized(); return; }
            syncActiveNav(win, doc);
            if (root) root.hidden = false;
            mountPageModules(win);
        } catch (error) {
            if (version !== generation || error.name === 'AbortError') return;
            unauthorized();
        }
    }
    function canLeave() { return win.TelegraphDownloaderHome?.canLeave?.() !== false && win.TelegraphDownloaderKomga?.canLeave?.() !== false; }
    function start() {
        session.mount();
        navigation.mount();
        return mount({ revalidate: true });
    }
    function beforeSwap(event) {
        if (!event.defaultPrevented && event.detail?.shouldSwap !== false && event.detail?.target?.id === 'content') {
            generation += 1;
            navigation.close();
            unmountPageModules(win);
        }
    }
    function beforeRequest(event) {
        if (event.detail?.target?.id === 'content' && !canLeave()) event.preventDefault();
    }
    function configRequest(event) {
        const method = String(event.detail?.verb || 'GET').toUpperCase();
        if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
            if (!session.isAuthenticated()) { event.preventDefault(); unauthorized(); return; }
            event.detail.headers['X-CSRF-Token'] = session.getCSRFToken();
        }
    }
    function afterSwap(event) { if (event.detail?.target?.id === 'content') return mount(); }
    function restore() { generation += 1; unmountPageModules(win); return mount({ revalidate: true }); }
    function pagehide() {
        navigation.close();
        generation += 1;
        unmountPageModules(win);
        session.clear();
        const root = content();
        if (root) root.hidden = true;
    }
    return { start, beforeSwap, beforeRequest, configRequest, afterSwap, restore, pagehide, unauthorized, canLeave };
}
