import { createTasksApi } from '../shared/api/tasks_api.js';

const messages = Object.freeze({
    invalid_input: '连接设置无效。更换地址时请同时替换 API Key。',
    config_conflict: '连接已在别处更新，请重新读取后再保存。',
    unauthorized: 'Komga 拒绝了 API Key，请检查授权。',
    forbidden: '当前 API Key 无法读取允许的书库。',
    komga_not_configured: '请先保存 Komga 地址和 API Key。',
    unreachable: '无法连接 Komga，请检查地址和网络。',
    timeout: 'Komga 连接超时，请稍后重试。',
    unsupported_version: '当前 Komga 版本不支持所需接口。',
    redirect_blocked: 'Komga 请求发生重定向，已停止测试。',
});

function errorMessage(result, fallback) {
    const code = result?.payload?.code;
    return typeof code === 'string' && messages[code] ? messages[code] : fallback;
}

export function createKomgaConnectionModule(win = window, doc = document, suppliedApi = null) {
    const transport = createTasksApi((url, options) => win.fetch(url, options), { win });
    const api = suppliedApi || {
        get: options => transport.getJson('/api/settings/komga', options),
        save: (input, options) => transport.putJson('/api/settings/komga', input, options),
        test: (version, options) => transport.postJson('/api/settings/komga/test', { config_version: version }, options),
    };
    let controls;
    let saved;
    let active = false;
    let generation = 0;
    let revision = 0;
    let reading;
    let saving;
    let testing;
    const listeners = [];

    function listen(node, type, handler) {
        node.addEventListener(type, handler);
        listeners.push(() => node.removeEventListener(type, handler));
    }
    function feedback(message) { if (controls) controls.feedback.textContent = message; }
    function updateControls() {
        if (!controls) return;
        controls.save.disabled = !saved || Boolean(saving) || Boolean(reading);
        controls.test.disabled = !saved?.credential_configured || !saved?.base_url || Boolean(testing) || Boolean(saving) || Boolean(reading);
    }
    function updateKeyRow() {
        if (!controls) return;
        const replace = controls.action.value === 'replace';
        controls.keyRow.hidden = !replace;
        controls.key.disabled = !replace;
        if (!replace) controls.key.value = '';
    }
    function edited() {
        revision += 1;
        testing?.abort(); testing = null;
        if (reading) { reading.abort(); reading = null; }
        feedback('当前连接设置尚未保存。');
        updateControls();
    }
    async function load() {
        if (!active || reading || saving) return;
        const run = generation;
        const before = revision;
        const controller = new AbortController();
        reading = controller;
        updateControls();
        feedback('正在读取 Komga 连接…');
        try {
            const result = await api.get({ signal: controller.signal });
            if (!active || run !== generation || controller.signal.aborted || revision !== before) return;
            const next = result?.payload;
            if (!result?.response?.ok || !next || !Number.isSafeInteger(next.config_version) || typeof next.base_url !== 'string' || typeof next.credential_configured !== 'boolean') {
                feedback(errorMessage(result, '连接读取失败，请重试。'));
                return;
            }
            saved = next;
            controls.base.value = next.base_url;
            controls.action.value = 'keep';
            controls.key.value = '';
            controls.keyState.textContent = next.credential_configured ? 'API Key 已配置，原值不会显示。' : '尚未配置 API Key。';
            updateKeyRow();
            feedback('');
        } catch (error) {
            if (active && run === generation && !controller.signal.aborted) feedback('连接读取失败，请重试。');
        } finally {
            if (reading === controller) reading = null;
            if (active && run === generation) updateControls();
        }
    }
    async function save(event) {
        event.preventDefault();
        if (!active || !saved || saving || reading) return;
        const credential = { action: controls.action.value };
        if (credential.action === 'replace') {
            if (!controls.key.value) { feedback('请输入新的 API Key。'); controls.key.focus(); return; }
            credential.value = controls.key.value;
        }
        const run = generation;
        const before = revision;
        const controller = new AbortController();
        const input = { expected_version: saved.config_version, base_url: controls.base.value.trim(), credential };
        saving = controller;
        testing?.abort(); testing = null;
        updateControls();
        feedback('正在保存连接…');
        try {
            const result = await api.save(input, { signal: controller.signal });
            if (!active || run !== generation || controller.signal.aborted) return;
            const next = result?.payload;
            if (!result?.response?.ok || !next || !Number.isSafeInteger(next.config_version)) {
                feedback(errorMessage(result, '连接保存失败，请重试。'));
                return;
            }
            saved = next;
            if (revision === before) {
                controls.base.value = next.base_url;
                controls.action.value = 'keep';
                controls.key.value = '';
                controls.keyState.textContent = next.credential_configured ? 'API Key 已配置，原值不会显示。' : '尚未配置 API Key。';
                updateKeyRow();
            }
            feedback(revision === before ? 'Komga 连接已保存。' : '已保存提交时的连接，后续编辑尚未保存。');
        } catch (error) {
            if (active && run === generation && !controller.signal.aborted) feedback('连接保存失败，请重试。');
        } finally {
            if (saving === controller) saving = null;
            if (active && run === generation) updateControls();
        }
    }
    async function test() {
        if (!active || !saved?.credential_configured || saving || reading) return;
        if (controls.base.value.trim() !== saved.base_url || controls.action.value !== 'keep') {
            feedback('请先保存当前编辑，再测试已保存的连接。');
            return;
        }
        testing?.abort();
        const controller = new AbortController();
        const run = generation;
        const before = revision;
        const version = saved.config_version;
        testing = controller;
        updateControls();
        feedback('正在测试已保存的连接…');
        try {
            const result = await api.test(version, { signal: controller.signal });
            if (!active || run !== generation || controller.signal.aborted || revision !== before) return;
            if (!result?.response?.ok || result.payload?.status !== 'connected') {
                feedback(errorMessage(result, 'Komga 连接测试失败。'));
                return;
            }
            feedback(`连接可用，当前允许访问 ${Number(result.payload.allowed_library_count) || 0} 个书库。`);
        } catch (error) {
            if (active && run === generation && !controller.signal.aborted) feedback('Komga 连接测试失败。');
        } finally {
            if (testing === controller) testing = null;
            if (active && run === generation) updateControls();
        }
    }
    function mount() {
        unmount();
        const root = doc.getElementById('komga-connection');
        if (!root) return;
        controls = {
            form: doc.getElementById('komga-connection-form'),
            base: doc.getElementById('komga-base-url'),
            action: doc.getElementById('komga-credential-action'),
            keyRow: doc.getElementById('komga-new-key-row'),
            key: doc.getElementById('komga-new-key'),
            keyState: doc.getElementById('komga-credential-state'),
            save: doc.getElementById('komga-save'),
            test: doc.getElementById('komga-test'),
            reload: doc.getElementById('komga-reload'),
            feedback: doc.getElementById('komga-connection-feedback'),
        };
        if (Object.values(controls).some(value => !value)) { controls = null; return; }
        active = true;
        listen(controls.form, 'submit', save);
        listen(controls.form, 'input', edited);
        listen(controls.base, 'change', edited);
        listen(controls.action, 'change', () => { updateKeyRow(); edited(); });
        listen(controls.test, 'click', test);
        listen(controls.reload, 'click', () => {
            if (controls.key.value || controls.base.value.trim() !== (saved?.base_url || '') || controls.action.value !== 'keep') {
                if (!win.confirm?.('重新读取会丢弃当前未保存的 Komga 连接设置，确定继续吗？')) return;
            }
            revision += 1;
            void load();
        });
        void load();
    }
    function unmount() {
        active = false;
        generation += 1;
        reading?.abort(); saving?.abort(); testing?.abort();
        reading = null; saving = null; testing = null;
        for (const remove of listeners.splice(0)) remove();
        if (controls) controls.key.value = '';
        controls = null;
        saved = null;
        revision = 0;
    }
    return { mount, unmount };
}
