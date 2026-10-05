import { createTasksApi } from '../shared/api/tasks_api.js';
import { acceptAttempt, telegramMessages, terminalAttempt } from './state.js';

export function createTelegramModule(win = window, doc = document, suppliedApi = null) {
    const transport = createTasksApi((url, options) => win.fetch(url, options), { win });
    const api = suppliedApi || {
        account: options => transport.getJson('/api/telegram/account', options),
        verify: options => transport.postJson('/api/telegram/account/verify', {}, options),
        start: options => transport.postJson('/api/telegram/login-attempts', {}, options),
        attempt: (id, options) => transport.getJson(`/api/telegram/login-attempts/${id}`, options),
        password: (id, password, options) => transport.postJson(`/api/telegram/login-attempts/${id}/password`, { password }, options),
        cancel: (id, options) => transport.postJson(`/api/telegram/login-attempts/${id}/cancel`, {}, options),
    };
    let root, controller, source, snapshot, controls;
    let generation = 0;
    let accountSequence = 0;
    let mounted = false;
    let mutation = false;
    let feedback = '';
    let timer = null;
    let retry = null;
    const removers = [];
    function element(tag, text, attrs = {}) {
        const node = doc.createElement(tag);
        if (text) node.textContent = text;
        for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, String(value));
        return node;
    }
    function listen(node, event, handler) { node.addEventListener(event, handler); removers.push(() => node.removeEventListener(event, handler)); }
    function message(code) { return telegramMessages[code] || telegramMessages.unavailable; }
    function clearQR() { if (controls) { controls.qr.removeAttribute('src'); controls.qr.hidden = true; } }
    function closeStream() { source?.close(); source = null; if (retry !== null) win.clearTimeout(retry); retry = null; }
    function setFeedback(value) { feedback = value; render(); }
    function render() {
        if (!controls) return;
        clearQR();
        const active = snapshot && !terminalAttempt(snapshot.state);
        controls.connect.disabled = mutation || Boolean(active);
        controls.verify.disabled = mutation || Boolean(active);
        controls.cancel.hidden = !active;
        controls.cancel.disabled = mutation;
        controls.passwordForm.hidden = snapshot?.state !== 'password_required';
        controls.passwordSubmit.disabled = mutation;
        if (snapshot?.state !== 'password_required') controls.password.value = '';
        controls.attempt.textContent = feedback || (snapshot ? message(snapshot.code || snapshot.state) : '');
        if (!snapshot) return;
        if (snapshot.qr && Date.parse(snapshot.qr_expires_at) > Date.now() && Date.parse(snapshot.expires_at) > Date.now()) {
            controls.qr.src = snapshot.qr; controls.qr.hidden = false;
            controls.expiry.textContent = `二维码将在 ${Math.max(0, Math.ceil((Date.parse(snapshot.qr_expires_at) - Date.now()) / 1000))} 秒后刷新。`;
        } else controls.expiry.textContent = snapshot.state === 'waiting_qr' ? '正在等待新的二维码…' : '';
        if (Date.parse(snapshot.expires_at) <= Date.now() && active) { clearQR(); controls.password.value = ''; controls.passwordForm.hidden = true; controls.attempt.textContent = '本次连接已到期，正在等待服务器确认结束。'; }
    }
    function consume(payload, allowNew = false) {
        const next = acceptAttempt(allowNew ? null : snapshot, payload);
        if (!next || next === snapshot) return;
        snapshot = next; feedback = ''; render();
        if (terminalAttempt(snapshot.state)) { closeStream(); if (snapshot.state === 'connected') refreshAccount(); }
    }
    function connectStream(id) {
        closeStream();
        const version = generation;
        const connection = new win.EventSource(`/api/telegram/login-attempts/${id}/events`);
        source = connection;
        connection.onmessage = event => {
            if (!mounted || generation !== version || snapshot?.attempt_id !== id || source !== connection) return;
            try { consume(JSON.parse(event.data)); } catch { setFeedback('账号状态响应无效，请刷新后重试。'); }
        };
        connection.onerror = () => {
            if (!mounted || generation !== version || snapshot?.attempt_id !== id || source !== connection) return;
            closeStream();
            // Authenticated fetch handles 401 through the shared lifecycle. Only
            // current snapshots are recovered; QR/2FA events are never cached.
            retry = win.setTimeout(async () => {
                try {
                    const { response, payload } = await api.attempt(id, { signal: controller.signal });
                    if (!mounted || generation !== version || snapshot?.attempt_id !== id) return;
                    if (!response.ok) { setFeedback(message(payload?.code)); return; }
                    consume(payload); if (!terminalAttempt(snapshot?.state)) connectStream(id);
                } catch { if (mounted && generation === version) setFeedback('连接暂时中断，请刷新账号状态。'); }
            }, 2000);
        };
    }
    async function refreshAccount() {
        const version = generation; const sequence = ++accountSequence;
        try {
            const { response, payload } = await api.account({ signal: controller.signal });
            if (!mounted || generation !== version || sequence !== accountSequence) return;
            if (!response.ok) { controls.account.textContent = message(payload?.code); return; }
            controls.account.textContent = `${message(payload.state)}${payload.busy ? ` ${message('busy')}` : ''}`;
            controls.connect.textContent = payload.revision > 0 ? '重新连接账号' : '扫码连接账号';
            if (Number.isSafeInteger(payload.max_source_bytes) && payload.max_source_bytes > 0) controls.limit.textContent = `当前单个附件上限：${(payload.max_source_bytes / (1024 * 1024)).toLocaleString('zh-CN', { maximumFractionDigits: 2 })} MiB。`;
            if (payload.verified_at) controls.verified.textContent = `上次授权验证：${new Date(payload.verified_at).toLocaleString('zh-CN')}`;
            if (payload.attempt && (!snapshot || payload.attempt.attempt_id === snapshot.attempt_id)) {
                consume(payload.attempt, !snapshot);
                if (snapshot && !terminalAttempt(snapshot.state) && !source) connectStream(snapshot.attempt_id);
            }
        } catch (error) { if (mounted && generation === version && error.name !== 'AbortError') controls.account.textContent = message('unavailable'); }
    }
    async function mutate(action) {
        if (mutation) return;
        mutation = true; feedback = ''; accountSequence += 1; render();
        const version = generation;
        try { await action(version); }
        catch (error) { if (mounted && generation === version && error.name !== 'AbortError') setFeedback(message('unavailable')); }
        finally { if (mounted && generation === version) { mutation = false; render(); } }
    }
    function mount() {
        unmount(); root = doc.querySelector('[data-module-slot="telegram-account"]'); if (!root) return;
        mounted = true; controller = new AbortController();
        const heading = element('h3', 'Telegram 账号');
        const account = element('p', '正在读取账号状态…', { id: 'telegram-account-status', role: 'status', 'aria-live': 'polite' });
        const verified = element('p', '', { class: 'field-help' });
        const limit = element('p', '', { class: 'field-help' });
        const connect = element('button', '扫码连接账号', { id: 'telegram-connect', type: 'button' });
        const verify = element('button', '重新验证授权', { id: 'telegram-verify', type: 'button', class: 'btn-secondary' });
        const attempt = element('p', '', { id: 'telegram-attempt-status', role: 'status', 'aria-live': 'polite' });
        const qr = element('img', '', { alt: 'Telegram 登录二维码', width: 256, height: 256 }); qr.hidden = true;
        const expiry = element('p', '', { class: 'field-help' });
        const passwordForm = element('form', '', { 'hx-history': 'false' }); passwordForm.hidden = true;
        const passwordLabel = element('label', 'Telegram 两步验证密码', { for: 'telegram-password' });
        const password = element('input', '', { id: 'telegram-password', type: 'password', autocomplete: 'off', maxlength: 1024, required: true });
        const passwordSubmit = element('button', '提交两步验证密码', { type: 'submit' }); passwordForm.append(passwordLabel, password, passwordSubmit);
        const cancel = element('button', '取消本次连接', { id: 'telegram-cancel', type: 'button', class: 'btn-secondary' }); cancel.hidden = true;
        controls = { account, verified, limit, connect, verify, attempt, qr, expiry, passwordForm, password, passwordSubmit, cancel };
        root.replaceChildren(heading, account, verified, limit, connect, verify, attempt, qr, expiry, passwordForm, cancel);
        listen(connect, 'click', () => mutate(async version => {
            const { response, payload } = await api.start({ signal: controller.signal });
            if (!mounted || generation !== version) return;
            if (!response.ok) { setFeedback(message(payload?.code)); return; }
            consume(payload, true); if (snapshot && !terminalAttempt(snapshot.state)) connectStream(snapshot.attempt_id);
        }));
        listen(verify, 'click', () => mutate(async version => {
            const { response, payload } = await api.verify({ signal: controller.signal });
            if (!mounted || generation !== version) return;
            if (!response.ok) { controls.account.textContent = message(payload?.code); return; }
            refreshAccount();
        }));
        listen(passwordForm, 'submit', event => {
            event.preventDefault(); if (mutation || snapshot?.state !== 'password_required') return;
            const value = password.value; password.value = '';
            mutate(async version => {
                const { response, payload } = await api.password(snapshot.attempt_id, value, { signal: controller.signal });
                if (!mounted || generation !== version) return;
                if (!response.ok) setFeedback(message(payload?.code));
            });
        });
        listen(cancel, 'click', () => mutate(async version => {
            const { response, payload } = await api.cancel(snapshot.attempt_id, { signal: controller.signal });
            if (!mounted || generation !== version) return;
            if (!response.ok) { setFeedback(message(payload?.code)); return; }
            consume(payload);
        }));
        timer = win.setInterval(render, 1000); refreshAccount();
    }
    function unmount() {
        mounted = false; generation += 1; accountSequence += 1; controller?.abort(); controller = null;
        closeStream(); if (timer !== null) win.clearInterval(timer); if (retry !== null) win.clearTimeout(retry); timer = null; retry = null;
        for (const remove of removers.splice(0)) remove();
        clearQR(); if (controls) controls.password.value = ''; root?.replaceChildren(); root = null; controls = null; snapshot = null; mutation = false; feedback = '';
    }
    return { mount, unmount };
}
