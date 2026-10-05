import { getAdminSession } from '../shared/admin_session.js';
import { createIntegrationsApi, createRequestScope, integrationError } from './integrations_api.js';

export function createIntegrationsModule(win = window, doc = document, suppliedApi = null, { sections = ['sources', 'ai', 'security'] } = {}) {
    const api = suppliedApi || createIntegrationsApi(win, doc);
    let mounted = false;
    let visible = false;
    let generation = 0;
    let hydration = null;
    const cleanups = [];
    const scopes = [];
    const roots = [];
    const pauseHandlers = [];
    const hydrationTargets = new Map();
    const loaded = new Set();
    const loading = new Set();

    function element(tag, text = '', attrs = {}) {
        const node = doc.createElement(tag);
        if (text) node.textContent = text;
        for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, String(value));
        return node;
    }
    function listen(node, type, handler) { node.addEventListener(type, handler); cleanups.push(() => node.removeEventListener(type, handler)); }
    function input(form, label, type, value = '') {
        const wrapper = element('label', label, { class: 'settings-row' });
        const control = element('input', '', { type });
        if (type === 'checkbox') control.checked = Boolean(value); else control.value = String(value);
        wrapper.append(control);
        form.append(wrapper);
        return control;
    }
    function select(form, label, entries, value) {
        const wrapper = element('label', label, { class: 'settings-row' });
        const control = element('select');
        for (const [key, text] of entries) control.append(element('option', text, { value: key }));
        control.value = value;
        wrapper.append(control); form.append(wrapper); return control;
    }
    function button(form, text, type = 'button') { const node = element('button', text, { type, class: 'btn btn-secondary' }); form.append(node); return node; }
    function status(form) { const node = element('p', '', { role: 'status', 'aria-live': 'polite', class: 'settings-help' }); form.append(node); return node; }
    function lock(form, value) { for (const control of form.querySelectorAll('input,select,button')) control.disabled = value; }
    function credentialControls(form, configured, allowed) {
        const indicator = element('p', configured ? '凭据已配置，保存时不会显示原值。' : allowed ? '尚未配置凭据。此服务可使用匿名请求。' : '此来源无需凭据。');
        form.append(indicator);
        if (!allowed) return { payload: () => ({ action: 'keep' }), clear() {} };
        const action = select(form, '凭据操作', [['keep', '保留现有凭据'], ['replace', '替换凭据'], ['clear', '清除凭据']], 'keep');
        const value = input(form, '新凭据（仅替换时填写）', 'password');
        const row = value.parentElement;
        value.autocomplete = 'off';
        value.maxLength = 4096;
        const showReplacement = () => { row.hidden = action.value !== 'replace'; value.hidden = row.hidden; };
        listen(action, 'change', () => { value.value = ''; showReplacement(); });
        showReplacement();
        return {
            payload() { return action.value === 'replace' ? { action: action.value, value: value.value } : { action: action.value }; },
            clear(nextConfigured) { value.value = ''; action.value = 'keep'; showReplacement(); indicator.textContent = nextConfigured ? '凭据已配置，保存时不会显示原值。' : '尚未配置凭据。'; },
        };
    }
    function sourceCard(root, original) {
        if (!original || typeof original.provider_id !== 'string' || !Number.isSafeInteger(original.config_version)) return;
        let saved = original;
        let editVersion = 0;
        let saving = false;
        const descriptor = original.descriptor || {};
        const form = element('form', '', { class: 'integration-card', 'hx-history': 'false' });
        const card = element('details', '', { class: 'source-card' });
        const heading = element('summary', descriptor.label || original.provider_id, { class: 'source-card-heading' });
        const state = element('span', saved.enabled ? '已启用' : '未启用', { class: 'source-card-state' });
        heading.append(state); card.append(heading);
        const enabled = input(form, '启用来源', 'checkbox', saved.enabled);
        const priority = input(form, '优先级（0–1000，越小越靠前）', 'number', saved.priority);
        priority.min = '0'; priority.max = '1000'; priority.step = '1'; priority.required = true;
        const filters = {};
        const preferences = {};
        for (const [name, values] of Object.entries(descriptor.filters || {})) {
            filters[name] = select(form, `筛选：${name}`, [['', '默认'], ...values.map((value) => [value, value])], saved.filters?.[name] || '');
        }
        for (const [name, values] of Object.entries(descriptor.field_preferences || {})) {
            preferences[name] = select(form, `字段偏好：${name}`, [['', '默认'], ...values.map((value) => [value, value])], saved.field_preferences?.[name] || '');
        }
        const credential = credentialControls(form, saved.credential_configured, descriptor.auth_modes?.includes('bearer'));
        const save = button(form, '保存来源设置', 'submit');
        const test = button(form, '测试已保存的来源');
        const feedback = status(form);
        const testFeedback = status(form);
        const scope = createRequestScope(); scopes.push(scope);
        const saveScope = createRequestScope(); scopes.push(saveScope);
        pauseHandlers.push(() => { scope.invalidate(); testFeedback.textContent = '远程检查已暂停，可重新发起测试。'; });
        listen(form, 'input', () => { editVersion++; scope.invalidate(); testFeedback.textContent = '当前编辑尚未保存；旧测试结果已失效。'; });
        listen(form, 'change', () => { editVersion++; scope.invalidate(); testFeedback.textContent = '当前编辑尚未保存；旧测试结果已失效。'; });
        const options = (controls) => Object.fromEntries(Object.entries(controls).filter(([, control]) => control.value !== '').map(([key, control]) => [key, control.value]));
        listen(form, 'submit', async (event) => {
            event.preventDefault();
            if (saving || !visible) return;
            if (!Number.isInteger(Number(priority.value)) || priority.value === '') { feedback.textContent = '请输入整数优先级。'; priority.focus(); return; }
            const request = saveScope.start('save');
            const submittedVersion = editVersion;
            scope.invalidate();
            const payload = { expected_version: saved.config_version, enabled: enabled.checked, priority: Number(priority.value), filters: options(filters), field_preferences: options(preferences), credential: credential.payload() };
            saving = true; save.disabled = true; feedback.textContent = '正在保存…';
            try {
                const { response, payload: result } = await api.saveSource(saved.provider_id, payload, { signal: request.signal });
                if (!request.current()) return;
                if (!response.ok || !result || !Number.isSafeInteger(result.config_version)) { feedback.textContent = integrationError(result); return; }
                saved = result; state.textContent = saved.enabled ? '已启用' : '未启用';
                if (editVersion === submittedVersion) credential.clear(saved.credential_configured);
                feedback.textContent = editVersion === submittedVersion ? '来源设置已保存。' : '已保存提交时的设置，后续编辑尚未保存。'; testFeedback.textContent = '';
            } catch (error) { if (request.current() && error.name !== 'AbortError') feedback.textContent = '保存失败，请稍后重试。'; }
            finally { saving = false; if (mounted && request.current()) save.disabled = false; }
        });
        listen(test, 'click', async () => {
            if (!visible || saving) return;
            const request = scope.start('test');
            testFeedback.textContent = '正在测试已保存设置…';
            try {
                const { response, payload } = await api.testSource(saved.provider_id, saved.config_version, { signal: request.signal });
                if (!request.current()) return;
                if (!response.ok || payload?.config_version !== saved.config_version) { testFeedback.textContent = integrationError(payload); return; }
                const scopeMessage = payload.scope === 'public_catalog_only' ? '仅验证公开目录可访问，未验证成人内容或受限条目权限。' : '';
                testFeedback.textContent = payload.status === 'passed' ? `连接测试通过。${scopeMessage}` : '当前不可测试，请稍后重试。';
            } catch (error) { if (request.current() && error.name !== 'AbortError') testFeedback.textContent = '测试失败，可稍后重试。'; }
        });
        card.append(form); root.append(card);
    }
    function aiForm(root, original) {
        if (!original || !Number.isSafeInteger(original.config_version)) { root.textContent = 'AI 设置响应无效。'; return; }
        let saved = original;
        let dirty = false;
        let editVersion = 0;
        let saving = false;
        const saveScope = createRequestScope(); scopes.push(saveScope);
        const scope = createRequestScope(); scopes.push(scope);
        const form = element('form', '', { class: 'integration-card', 'hx-history': 'false' });
        const enabled = input(form, '启用 AI 辅助提取', 'checkbox', saved.enabled);
        const base = input(form, 'API Base URL', 'url', saved.base_url); base.placeholder = 'http://本地服务:端口/v1';
        const model = input(form, '模型 ID（可手动填写）', 'text', saved.model_id); model.maxLength = 256;
        model.placeholder = '例如 minicpm5-2b-q4';
        const models = select(form, '已发现的模型', [['', '保留当前模型 ID']], '');
        const credential = credentialControls(form, saved.credential_configured, true);
        const save = button(form, '保存 AI 设置', 'submit');
        const refresh = button(form, '刷新模型列表');
        const test = button(form, '测试已保存的模型');
        const feedback = status(form);
        const discovery = status(form);
        const testFeedback = status(form);
        if (typeof doc.addEventListener === 'function') listen(doc, 'metadata-definitions-changed', () => {
            scope.cancel('test');
            testFeedback.textContent = '字段定义已更新，请重新测试模型。';
        });
        pauseHandlers.push(() => { scope.invalidate(); discovery.textContent = '模型发现已暂停，可按需刷新。'; testFeedback.textContent = '远程检查已暂停，可重新发起测试。'; });
        function edited() { editVersion++; dirty = true; scope.invalidate(); discovery.textContent = '配置已编辑，请先保存后刷新模型列表。'; testFeedback.textContent = '旧测试结果已失效。'; }
        listen(form, 'input', edited); listen(form, 'change', edited);
        listen(models, 'change', () => { if (models.value) model.value = models.value; });
        async function discover() {
            if (!visible || saving) return;
            if (dirty) { discovery.textContent = '请先保存设置，再读取模型列表。'; return; }
            if (!saved.base_url) { discovery.textContent = '保存 API 地址后可读取模型列表，也可手动填写模型 ID。'; return; }
            const request = scope.start('models');
            const version = saved.config_version;
            discovery.textContent = '正在读取模型列表，不会发送识别文字…';
            try {
                const { response, payload } = await api.models(version, { signal: request.signal });
                if (!request.current() || saved.config_version !== version) return;
                if (!response.ok || payload?.config_version !== version || !Array.isArray(payload.models)) { discovery.textContent = integrationError(payload); return; }
                models.replaceChildren(element('option', '保留当前模型 ID', { value: '' }));
                for (const item of payload.models) {
                    if (typeof item.id !== 'string' || item.id.length > 256) continue;
                    const option = element('option', `${item.id}${item.capability === 'unknown' ? '（能力未验证）' : ''}`, { value: item.id });
                    option.disabled = item.selectable !== true;
                    models.append(option);
                }
                discovery.textContent = payload.models.length === 0 ? '服务未返回模型，可保留或手动填写模型 ID。' : saved.model_id && !payload.models.some((item) => item.id === saved.model_id) ? '已保存的模型不在列表中，原 ID 已保留。' : '模型列表已更新。请选择后保存，推理能力需要单独测试。';
            } catch (error) { if (request.current() && error.name !== 'AbortError') discovery.textContent = '无法读取模型列表，可保留或手动填写模型 ID。'; }
        }
        listen(form, 'submit', async (event) => {
            event.preventDefault();
            if (saving || !visible) return;
            scope.invalidate();
            const submittedVersion = editVersion;
            const request = saveScope.start('save');
            const payload = { expected_version: saved.config_version, enabled: enabled.checked, base_url: base.value.trim(), model_id: model.value.trim(), credential: credential.payload() };
            saving = true; save.disabled = true; feedback.textContent = '正在保存…';
            try {
                const { response, payload: result } = await api.saveAI(payload, { signal: request.signal });
                if (!request.current()) return;
                if (!response.ok || !result || !Number.isSafeInteger(result.config_version)) { feedback.textContent = integrationError(result); return; }
                saved = result; dirty = editVersion !== submittedVersion;
                if (!dirty) { base.value = result.base_url; model.value = result.model_id; credential.clear(result.credential_configured); }
                feedback.textContent = dirty ? '已保存提交时的设置，后续编辑尚未保存。' : 'AI 设置已保存。'; testFeedback.textContent = '';
            } catch (error) { if (request.current() && error.name !== 'AbortError') feedback.textContent = '保存失败，请稍后重试。'; }
            finally { saving = false; if (mounted && request.current()) save.disabled = false; }
        });
        listen(refresh, 'click', discover);
        listen(test, 'click', async () => {
            if (!visible || saving) return;
            if (dirty) { testFeedback.textContent = '请先保存模型设置，再进行测试。'; return; }
            const request = scope.start('test');
            const version = saved.config_version;
            testFeedback.textContent = '正在用固定无敏感样例验证模型，最长等待约两分钟…';
            try {
                const { response, payload } = await api.testAI(version, saved.model_id, { signal: request.signal });
                if (!request.current() || saved.config_version !== version) return;
                if (!response.ok || payload?.config_version !== version || payload?.model_id !== saved.model_id) { testFeedback.textContent = integrationError(payload); return; }
                testFeedback.textContent = payload.status === 'passed' ? `固定样例验证通过，测试覆盖 ${Array.isArray(payload.field_keys) ? payload.field_keys.length : 0} 个字段；实际识别结果仍需确认。` : '模型测试未通过。';
            } catch (error) { if (request.current() && error.name !== 'AbortError') testFeedback.textContent = '模型测试失败，可稍后重试。'; }
        });
        root.append(form);
        discover();
    }
    function passwordForm(root) {
        let saving = false;
        const scope = createRequestScope(); scopes.push(scope);
        const form = element('form', '', { class: 'integration-card', 'hx-history': 'false' });
        form.append(element('p', '修改密码后，所有已登录会话将立即失效，需要重新登录。'));
        const current = input(form, '当前密码', 'password'); current.autocomplete = 'current-password'; current.required = true;
        const next = input(form, '新密码（至少12个字符，最多72字节）', 'password'); next.autocomplete = 'new-password'; next.required = true;
        const confirmation = input(form, '确认新密码', 'password'); confirmation.autocomplete = 'new-password'; confirmation.required = true;
        button(form, '修改密码并重新登录', 'submit');
        const feedback = status(form);
        listen(form, 'submit', async (event) => {
            event.preventDefault();
            if (saving || !visible) return;
            if (next.value !== confirmation.value) { feedback.textContent = '两次输入的新密码不一致。'; confirmation.value = ''; confirmation.focus(); return; }
            if (Array.from(next.value).length < 12 || new TextEncoder().encode(next.value).length > 72) { feedback.textContent = '新密码至少12个字符，且不能超过72字节。'; next.focus(); return; }
            const request = scope.start('password');
            const payload = { current_password: current.value, new_password: next.value };
            current.value = ''; next.value = ''; confirmation.value = '';
            saving = true; lock(form, true); feedback.textContent = '正在修改密码…';
            try {
                const { response, payload: result } = await api.changePassword(payload, { signal: request.signal });
                if (!request.current()) return;
                if (!response.ok) { feedback.textContent = integrationError(result); return; }
                getAdminSession(win, doc).endSession();
            } catch (error) { if (request.current() && error.name !== 'AbortError') feedback.textContent = '密码修改未完成，请重新确认当前密码后重试。'; }
            finally { saving = false; if (mounted && request.current()) lock(form, false); }
        });
        root.replaceChildren(form);
    }
    async function hydrate(root, kind, version, signal) {
        if (loaded.has(kind) || loading.has(kind)) return;
        loading.add(kind);
        root.textContent = '正在加载设置…';
        try {
            const { response, payload } = await (kind === 'sources' ? api.sources({ signal }) : api.ai({ signal }));
            if (!mounted || generation !== version || signal.aborted) return;
            if (!response.ok || !payload) { root.textContent = `设置加载失败。${integrationError(payload)} 返回此分类时将重试。`; return; }
            if (kind === 'sources') {
                if (!Array.isArray(payload.sources) || payload.sources.some(source => !source || typeof source.provider_id !== 'string' || !Number.isSafeInteger(source.config_version))) { root.textContent = '来源设置响应无效，返回此分类时将重试。'; return; }
                root.replaceChildren();
                for (const source of payload.sources) sourceCard(root, source);
            } else {
                if (!Number.isSafeInteger(payload.config_version)) { root.textContent = 'AI 设置响应无效，返回此分类时将重试。'; return; }
                root.replaceChildren();
                aiForm(root, payload);
            }
            loaded.add(kind);
        } catch (error) { if (mounted && generation === version && error.name !== 'AbortError') root.textContent = '设置加载失败，返回此分类时将重试。'; }
        finally { if (generation === version) loading.delete(kind); }
    }
    function mount() {
        if (mounted) { resume(); return; }
        const sourceRoot = sections.includes('sources') && doc.querySelector('[data-module-slot="source-settings"]');
        const aiRoot = sections.includes('ai') && doc.querySelector('[data-module-slot="ai-settings"]');
        const adminRoot = sections.includes('security') && doc.querySelector('[data-module-slot="admin-settings"]');
        if (!sourceRoot && !aiRoot && !adminRoot) return;
        mounted = true; visible = true; hydration = new AbortController();
        if (adminRoot) { roots.push(adminRoot); passwordForm(adminRoot); }
        if (sourceRoot) { roots.push(sourceRoot); hydrationTargets.set('sources', sourceRoot); }
        if (aiRoot) { roots.push(aiRoot); hydrationTargets.set('ai', aiRoot); }
        resume();
    }
    function pause() { if (!mounted || !visible) return; visible = false; pauseHandlers.forEach(pause => pause()); }
    function resume() {
        if (!mounted) return;
        visible = true;
        for (const [kind, root] of hydrationTargets) void hydrate(root, kind, generation, hydration.signal);
    }
    function unmount() {
        mounted = false; visible = false; generation += 1;
        hydration?.abort(); hydration = null;
        hydrationTargets.clear(); loaded.clear(); loading.clear();
        for (const scope of scopes.splice(0)) scope.dispose();
        pauseHandlers.length = 0;
        for (const remove of cleanups.splice(0)) remove();
        for (const root of roots.splice(0)) { for (const input of root.querySelectorAll('input[type="password"]')) input.value = ''; root.replaceChildren(); }
    }
    return { mount, unmount, pause, resume };
}
