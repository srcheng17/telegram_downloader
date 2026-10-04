// Session tokens stay in HttpOnly cookies; only the CSRF token lives in memory.
export function createAdminSession({ win = window, doc = document, fetchImpl } = {}) {
    const fetcher = fetchImpl || ((url, options) => win.fetch(url, options));
    let csrf = '';
    let authenticated = false;
    let generation = 0;
    let controller = null;
    let mutationController = null;
    let pending = null;
    let form = null;
    const listeners = [];

    function clearInputs() {
        for (const input of doc.querySelectorAll('input[type="password"]')) input.value = '';
    }
    function clear() {
        generation += 1;
        controller?.abort();
        mutationController?.abort();
        mutationController = null;
        controller = null;
        pending = null;
        csrf = '';
        authenticated = false;
        clearInputs();
    }
    function accept(payload) {
        if (!payload || typeof payload.authenticated !== 'boolean' || typeof payload.csrf_token !== 'string' || payload.csrf_token.length !== 43) {
            throw new Error('会话响应无效，请刷新重试');
        }
        csrf = payload.csrf_token;
        authenticated = payload.authenticated;
        return { authenticated, csrf_token: csrf, expires_at: payload.expires_at };
    }
    async function read(response) {
        const payload = await response.json().catch(() => null);
        if (!response.ok) throw new Error(response.status === 429 ? '请求过于频繁，请稍后重试' : '登录失败，请检查密码或刷新页面');
        return payload;
    }
    function refresh() {
        if (pending) return pending;
        const version = generation;
        const active = new AbortController();
        controller = active;
        pending = (async () => {
            const response = await fetcher('/api/auth/session', { method: 'GET', cache: 'no-store', credentials: 'same-origin', signal: active.signal });
            const payload = await read(response);
            if (version !== generation || active.signal.aborted) throw new DOMException('请求已取消', 'AbortError');
            return accept(payload);
        })().finally(() => {
            if (controller === active) { controller = null; pending = null; }
        });
        return pending;
    }
    async function mutate(url, body) {
        await refresh();
        const version = generation;
        mutationController?.abort();
        const active = new AbortController();
        mutationController = active;
        try {
            const response = await fetcher(url, {
                method: 'POST', cache: 'no-store', credentials: 'same-origin', signal: active.signal,
                headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
                ...(body ? { body: JSON.stringify(body) } : {}),
            });
            const payload = await read(response);
            if (version !== generation || active.signal.aborted) throw new DOMException('请求已取消', 'AbortError');
            return payload;
        } finally { if (mutationController === active) mutationController = null; }
    }
    async function login(password) { return accept(await mutate('/api/auth/login', { password })); }
    function endSession() {
        clear();
        if (typeof win.__onAdminUnauthorized === 'function') win.__onAdminUnauthorized();
        else win.location.assign('/auth/login');
    }
    async function logout() {
        await mutate('/api/auth/logout');
        endSession();
    }
    function feedback(message) { const node = doc.getElementById('login-feedback'); if (node) node.textContent = message; }
    async function submit(event) {
        event.preventDefault();
        const activeForm = event.currentTarget;
        const input = activeForm.querySelector('[name="password"]');
        const button = activeForm.querySelector('button[type="submit"]');
        if (button?.disabled) return;
        const password = input?.value || '';
        if (input) input.value = '';
        if (button) button.disabled = true;
        feedback('正在登录…');
        try {
            await login(password);
            if (form !== activeForm) return;
            win.location.assign('/');
        } catch (error) {
            if (form !== activeForm || error.name === 'AbortError') return;
            feedback(error.message);
            input?.focus();
        } finally { if (form === activeForm && button) button.disabled = false; }
    }
    function listen(node, event, handler) { node.addEventListener(event, handler); listeners.push(() => node.removeEventListener(event, handler)); }
    function mount() {
        unmount();
        form = doc.getElementById('admin-login-form');
        if (form) listen(form, 'submit', submit);
        for (const button of doc.querySelectorAll('[data-admin-logout]')) {
            listen(button, 'click', async () => {
                if (button.disabled) return;
                button.disabled = true;
                try { await logout(); } catch { button.disabled = false; button.textContent = '退出失败，重试'; }
            });
        }
        refresh().then((state) => { if (form && state.authenticated) win.location.replace('/'); })
            .catch((error) => { if (error.name !== 'AbortError' && form) feedback(error.message); });
    }
    function unmount() {
        for (const remove of listeners.splice(0)) remove();
        form = null;
        clear();
    }
    return { mount, unmount, refresh, login, logout, clear, endSession, getCSRFToken: () => csrf, isAuthenticated: () => authenticated };
}

export function getAdminSession(win = window, doc = document) {
    if (!win.__adminSession) win.__adminSession = createAdminSession({ win, doc });
    return win.__adminSession;
}
