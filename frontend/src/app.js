import { mountPageModules, syncActiveNav, unmountPageModules } from './shared/page_modules.js';

function syncNavigationState() {
    syncActiveNav(window, document);
}

document.addEventListener('DOMContentLoaded', () => {
    syncNavigationState();
    mountPageModules(window);
});

window.addEventListener('popstate', syncNavigationState);

document.body.addEventListener('htmx:beforeSwap', (event) => {
    if (event.detail && event.detail.target && event.detail.target.id === 'content') {
        unmountPageModules(window);
    }
});

document.body.addEventListener('htmx:afterSwap', (event) => {
    if (event.detail && event.detail.target && event.detail.target.id === 'content') {
        syncNavigationState();
        mountPageModules(window);
    }
});
