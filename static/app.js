(function () {
    function syncActiveNav() {
        const navLinks = document.querySelectorAll('.main-nav .nav-link');
        const currentPath = window.location.pathname;
        navLinks.forEach((link) => {
            const linkPath = new URL(link.href, window.location.origin).pathname;
            link.classList.toggle('active', linkPath === currentPath);
        });
    }

    function mountPageModules() {
        if (window.TelegraphDownloaderHome && typeof window.TelegraphDownloaderHome.mount === 'function') {
            window.TelegraphDownloaderHome.mount();
        }
        if (window.TelegraphDownloaderLogs && typeof window.TelegraphDownloaderLogs.mount === 'function') {
            window.TelegraphDownloaderLogs.mount();
        }
    }

    function unmountPageModules() {
        if (window.TelegraphDownloaderHome && typeof window.TelegraphDownloaderHome.unmount === 'function') {
            window.TelegraphDownloaderHome.unmount();
        }
        if (window.TelegraphDownloaderLogs && typeof window.TelegraphDownloaderLogs.unmount === 'function') {
            window.TelegraphDownloaderLogs.unmount();
        }
    }

    document.addEventListener('DOMContentLoaded', function () {
        syncActiveNav();
        mountPageModules();
    });

    window.addEventListener('popstate', syncActiveNav);

    document.body.addEventListener('htmx:beforeSwap', function (event) {
        if (event.detail && event.detail.target && event.detail.target.id === 'content') {
            unmountPageModules();
        }
    });

    document.body.addEventListener('htmx:afterSwap', function (event) {
        if (event.detail && event.detail.target && event.detail.target.id === 'content') {
            syncActiveNav();
            mountPageModules();
        }
    });
})();
