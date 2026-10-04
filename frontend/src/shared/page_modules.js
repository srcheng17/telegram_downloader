function getPageModules(win) {
    if (!win || typeof win !== 'object') {
        return [];
    }

    return [
        win.TelegraphDownloaderHome,
        win.TelegraphDownloaderLogs,
		win.TelegraphDownloaderKomga,
        win.TelegraphDownloaderSettings,
    ].filter(Boolean);
}

export function syncActiveNav(win, doc) {
    if (!win || !doc || typeof doc.querySelectorAll !== 'function') {
        return;
    }

    const currentPath = win.location && typeof win.location.pathname === 'string' ? win.location.pathname : '';
    const origin =
        win.location && typeof win.location.origin === 'string' && win.location.origin
            ? win.location.origin
            : 'http://localhost';

    doc.querySelectorAll('.main-nav .nav-link').forEach((link) => {
        if (!link || !link.classList || typeof link.classList.toggle !== 'function') {
            return;
        }

        let linkPath = '';
        try {
            linkPath = new URL(link.href, origin).pathname;
        } catch (error) {
            linkPath = '';
        }

        const active = linkPath === currentPath;
        link.classList.toggle('active', active);
        if (active) link.setAttribute?.('aria-current', 'page');
        else link.removeAttribute?.('aria-current');
    });
}

export function mountPageModules(win) {
    getPageModules(win).forEach((pageModule) => {
        if (typeof pageModule.mount === 'function') {
            pageModule.mount();
        }
    });
}

export function unmountPageModules(win) {
    getPageModules(win).forEach((pageModule) => {
        if (typeof pageModule.unmount === 'function') {
            pageModule.unmount();
        }
    });
}
