import { createAppLifecycle } from './shared/app_lifecycle.js';
import { syncActiveNav } from './shared/page_modules.js';

const app = createAppLifecycle(window, document);
document.addEventListener('DOMContentLoaded', () => app.start());
window.addEventListener('popstate', () => syncActiveNav(window, document));
window.addEventListener('pagehide', app.pagehide);
window.addEventListener('pageshow', event => { if (event.persisted) app.restore(); });
window.addEventListener('beforeunload', event => {
    if (window.TelegraphDownloaderHome?.hasUnsavedChanges?.() || window.TelegraphDownloaderKomga?.hasUnsavedChanges?.()) { event.preventDefault(); event.returnValue = ''; }
});
document.body.addEventListener('htmx:beforeRequest', app.beforeRequest);
document.body.addEventListener('htmx:configRequest', app.configRequest);
document.body.addEventListener('htmx:beforeSwap', app.beforeSwap);
document.body.addEventListener('htmx:afterSwap', app.afterSwap);
document.body.addEventListener('htmx:historyRestore', app.restore);
document.body.addEventListener('htmx:responseError', event => { if (event.detail?.xhr?.status === 401) app.unauthorized(); });
