import test from 'node:test';
import assert from 'node:assert/strict';
import { createIntegrationsModule } from '../settings/integrations.js';

function element(tag = 'div') {
    const listeners = new Map();
    const node = {
        tag, children: [], attrs: {}, textContent: '', value: '', checked: false, disabled: false,
        setAttribute(name, value) { this.attrs[name] = value; },
        append(...children) { for (const child of children) child.parentElement = this; this.children.push(...children); },
        replaceChildren(...children) { this.children = children; },
        addEventListener(name, listener) { listeners.set(name, listener); },
        removeEventListener(name) { listeners.delete(name); },
        emit(name) { return listeners.get(name)?.({ currentTarget: this, preventDefault() {} }); },
        focus() { this.focused = true; },
        querySelectorAll(selector) {
            const descendants = this.children.flatMap((child) => [child, ...child.querySelectorAll('*')]);
            if (selector === '*') return descendants;
            if (selector === 'input[type="password"]') return descendants.filter((child) => child.tag === 'input' && child.attrs.type === 'password');
            return descendants.filter((child) => selector.split(',').includes(child.tag));
        },
    };
    return node;
}
const flush = () => new Promise((resolve) => setImmediate(resolve));
const ok = (payload) => ({ response: { ok: true }, payload });
function moduleFixture(api, { admin = false, source = false } = {}) {
    const root = element();
    const events = new Map();
    const doc = {
        addEventListener(name, listener) { events.set(name, listener); },
        removeEventListener(name) { events.delete(name); },
        createElement: element,
        querySelector(selector) { return selector === `[data-module-slot="${admin ? 'admin' : source ? 'source' : 'ai'}-settings"]` ? root : null; },
    };
    let ended = 0;
    const win = { __adminSession: { endSession() { ended += 1; } } };
    const module = createIntegrationsModule(win, doc, api);
    module.mount();
    return { root, module, emit: name => events.get(name)?.(), ended: () => ended };
}

test('source probe localizes its known scope and never displays unknown upstream text', async (t) => {
    const cases = [
        { status: 'passed', scope: 'public_catalog_only', expected: '连接测试通过。仅验证公开目录可访问，未验证成人内容或受限条目权限。' },
        { status: 'passed', scope: 'upstream-private-detail', expected: '连接测试通过。' },
        { status: 'passed', scope: undefined, expected: '连接测试通过。' },
        { status: 'unavailable', scope: 'upstream-private-detail', expected: '当前不可测试，请稍后重试。' },
        { status: 'unexpected', scope: 'public_catalog_only', expected: '当前不可测试，请稍后重试。' },
    ];
    for (const scenario of cases) {
        await t.test(`${scenario.status}/${scenario.scope}`, async () => {
            const f = moduleFixture({
                sources: async () => ok({ sources: [{ provider_id: 'mangabaka', config_version: 2, enabled: true, priority: 0, descriptor: { label: 'MangaBaka' } }] }),
                testSource: async (id, version) => {
                    assert.equal(id, 'mangabaka');
                    assert.equal(version, 2);
                    return ok({ config_version: 2, status: scenario.status, scope: scenario.scope });
                },
            }, { source: true });
            try {
                await flush();
                const form = f.root.querySelectorAll('form')[0];
                const testButton = form.children.find(child => child.textContent === '测试已保存的来源');
                await testButton.emit('click');
                const feedback = form.children.filter(child => child.attrs.role === 'status').at(-1);
                assert.equal(feedback.textContent, scenario.expected);
            } finally {
                f.module.unmount();
            }
        });
    }
});

test('AI form preserves manual model edits and ignores discovery after editing/unmount', async () => {
    let resolveModels;
    const api = {
        ai: async () => ok({ enabled: true, base_url: 'http://localhost:8000', model_id: 'saved-model', config_version: 1, credential_configured: true }),
        models: () => new Promise((resolve) => { resolveModels = resolve; }),
    };
    const f = moduleFixture(api);
    await flush();
    const form = f.root.children[0];
    const model = form.children.find((child) => child.textContent.startsWith('模型 ID')).children[0];
    model.value = 'manual-new-model';
    form.emit('input');
    resolveModels(ok({ config_version: 1, models: [{ id: 'old-response-model', selectable: true, capability: 'text' }] }));
    await flush();
    assert.equal(model.value, 'manual-new-model');
    const modelList = form.querySelectorAll('select')[0];
    assert.equal(modelList.children.length, 1);
    assert.equal(form.children.some((child) => child.textContent.includes('配置已编辑')), true);
    const secret = form.querySelectorAll('input[type="password"]')[0];
    secret.value = 'transient-secret';
    f.module.unmount();
    assert.equal(secret.value, '');
    assert.equal(f.root.children.length, 0);
});

test('unmounted settings hydration never creates controls', async () => {
    let resolve;
    const f = moduleFixture({ ai: () => new Promise((done) => { resolve = done; }) });
    f.module.unmount();
    resolve(ok({ enabled: false, base_url: '', model_id: '', config_version: 0 }));
    await flush();
    assert.equal(f.root.children.length, 0);
});

test('password change clears inputs immediately and ends all local session state after success', async () => {
    let request;
    let finish;
    const api = { changePassword(input) { request = input; return new Promise((resolve) => { finish = resolve; }); } };
    const f = moduleFixture(api, { admin: true });
    const form = f.root.children[0];
    const inputs = form.querySelectorAll('input[type="password"]');
    inputs[0].value = 'current-synthetic-password';
    inputs[1].value = 'new-synthetic-password';
    inputs[2].value = inputs[1].value;
    const submission = form.emit('submit');
    assert.deepEqual(inputs.map((input) => input.value), ['', '', '']);
    assert.equal(request.current_password, 'current-synthetic-password');
    assert.equal(request.new_password, 'new-synthetic-password');
    finish(ok({ authenticated: false }));
    await submission;
    assert.equal(f.ended(), 1);
    f.module.unmount();
});


test('definition updates invalidate a pending AI test without changing configured model', async () => {
    let completeTest;
    const api = {
        ai: async () => ok({ enabled: true, base_url: 'http://localhost:8000', model_id: 'saved-model', config_version: 1 }),
        models: async () => ok({ config_version: 1, models: [] }),
        testAI: () => new Promise(resolve => { completeTest = resolve; }),
    };
    const f = moduleFixture(api);
    await flush();
    const form = f.root.children[0];
    const testButton = form.children.find(child => child.textContent === '测试已保存的模型');
    const pending = testButton.emit('click');
    f.emit('metadata-definitions-changed');
    completeTest(ok({ config_version: 1, model_id: 'saved-model', status: 'passed', field_keys: ['title'] }));
    await pending;
    assert.equal(form.children.some(child => child.textContent === '字段定义已更新，请重新测试模型。'), true);
    assert.equal(form.children.some(child => child.textContent.includes('固定样例验证通过')), false);
    f.module.unmount();
});

test('hiding AI aborts discovery and testing but preserves pending saves and later edits', async () => {
    let finishSave; let finishModels; let saveSignal; let modelsSignal; let savedInput; let requests = 0;
    const f = moduleFixture({
        ai: async () => ok({ enabled: true, base_url: 'http://localhost:8000', model_id: 'saved-model', config_version: 1 }),
        models: (_version, options) => { requests++; modelsSignal = options.signal; return new Promise(resolve => { finishModels = resolve; }); },
        saveAI: (input, options) => { savedInput = input; saveSignal = options.signal; return new Promise(resolve => { finishSave = resolve; }); },
    });
    await flush(); const form = f.root.children[0];
    const model = form.children.find(child => child.textContent.startsWith('模型 ID')).children[0];
    const save = form.children.find(child => child.textContent === '保存 AI 设置');
    model.value = 'submitted'; form.emit('input'); const pending = form.emit('submit');
    model.value = 'later-edit'; form.emit('input'); f.module.pause();
    assert.equal(modelsSignal.aborted, true); assert.equal(saveSignal.aborted, false); assert.equal(save.disabled, true);
    await form.emit('submit'); assert.equal(savedInput.model_id, 'submitted');
    finishSave(ok({ enabled: true, base_url: 'http://localhost:8000', model_id: 'submitted', config_version: 2 })); await pending;
    assert.equal(model.value, 'later-edit'); assert.equal(save.disabled, false); assert.equal(requests, 1);
    finishModels(ok({ config_version: 1, models: [{ id: 'late', selectable: true }] })); await flush();
    assert.equal(form.querySelectorAll('select')[0].children.length, 1);
    f.module.resume(); const retry = form.emit('submit'); assert.equal(savedInput.expected_version, 2); assert.equal(savedInput.model_id, 'later-edit');
    finishSave(ok({ enabled: true, base_url: 'http://localhost:8000', model_id: 'later-edit', config_version: 3 })); await retry;
    f.module.unmount();
});

test('section instances fetch only their requested settings and ignore probes after pause', async () => {
    const roots = { sources: element(), ai: element(), security: element() }; const calls = []; let complete; let signal;
    const doc = { createElement: element, querySelector: selector => ({ '[data-module-slot="source-settings"]': roots.sources, '[data-module-slot="ai-settings"]': roots.ai, '[data-module-slot="admin-settings"]': roots.security })[selector] };
    const api = { sources: async () => { calls.push('sources'); return ok({ sources: [{ provider_id: 'mangabaka', config_version: 1, enabled: true, priority: 0, descriptor: { label: 'MangaBaka' } }] }); }, ai: async () => { calls.push('ai'); return ok({ config_version: 0 }); }, testSource: (_id, _version, options) => { signal = options.signal; return new Promise(resolve => { complete = resolve; }); } };
    const module = createIntegrationsModule({}, doc, api, { sections: ['sources'] }); module.mount(); await flush();
    assert.deepEqual(calls, ['sources']); assert.equal(roots.ai.children.length, 0); assert.equal(roots.security.children.length, 0);
    const form = roots.sources.querySelectorAll('form')[0];
    const pending = form.children.find(child => child.textContent === '测试已保存的来源').emit('click'); module.pause();
    assert.equal(signal.aborted, true); complete(ok({ config_version: 1, status: 'passed' })); await pending;
    assert.ok(!form.children.some(child => child.textContent.startsWith('连接测试通过'))); module.resume(); module.unmount();
});

test('source save completes while hidden without reporting a false edit, then recovers from an error', async () => {
    let complete; let signal; let input; let saves = 0;
    const f = moduleFixture({
        sources: async () => ok({ sources: [{ provider_id: 'mangabaka', config_version: 4, enabled: true, priority: 0, descriptor: { label: 'MangaBaka' } }] }),
        saveSource: (_id, payload, options) => { saves++; input = payload; signal = options.signal; return new Promise(resolve => { complete = resolve; }); },
    }, { source: true });
    await flush(); const form = f.root.querySelectorAll('form')[0]; const save = form.children.find(child => child.textContent === '保存来源设置');
    const pending = form.emit('submit'); f.module.pause();
    assert.equal(signal.aborted, false); assert.equal(save.disabled, true);
    complete(ok({ provider_id: 'mangabaka', config_version: 5, enabled: true, priority: 0 })); await pending;
    assert.ok(form.children.some(child => child.textContent === '来源设置已保存。')); assert.equal(save.disabled, false);
    f.module.resume(); const failed = form.emit('submit'); assert.equal(input.expected_version, 5);
    complete({ response: { ok: false }, payload: { code: 'config_conflict' } }); await failed;
    assert.equal(save.disabled, false); assert.equal(saves, 2);
    const retry = form.emit('submit'); assert.equal(input.expected_version, 5); complete(ok({ config_version: 6, provider_id: 'mangabaka', enabled: true, priority: 0 })); await retry;
    assert.equal(save.disabled, false); f.module.unmount();
});

test('hidden first hydration does not begin AI discovery, and a resumed user can request it', async () => {
    let complete; let discoveries = 0;
    const f = moduleFixture({ ai: () => new Promise(resolve => { complete = resolve; }), models: async () => { discoveries++; return ok({ config_version: 1, models: [] }); } });
    f.module.pause(); complete(ok({ config_version: 1, base_url: 'http://localhost:8000', model_id: 'saved-model' })); await flush();
    assert.equal(discoveries, 0); f.module.resume();
    await f.root.children[0].children.find(child => child.textContent === '刷新模型列表').emit('click');
    assert.equal(discoveries, 1); f.module.unmount();
});

test('failed source and AI hydration retries on resume, then preserves successfully loaded edits', async () => {
    for (const kind of ['sources', 'ai']) for (const failure of ['http', 'network', 'malformed']) {
        let attempts = 0;
        const get = async () => {
            attempts++;
            if (attempts === 1) {
                if (failure === 'network') throw new Error('synthetic network error');
                if (failure === 'http') return { response: { ok: false }, payload: { code: 'unavailable' } };
                return ok(kind === 'sources' ? { sources: null } : {});
            }
            return ok(kind === 'sources' ? { sources: [{ provider_id: 'mangabaka', config_version: 1, enabled: true, priority: 0, descriptor: { label: 'MangaBaka' } }] } : { config_version: 1, base_url: '', model_id: 'saved-model' });
        };
        const f = moduleFixture({ [kind]: get }, { source: kind === 'sources' }); await flush();
        assert.equal(f.root.querySelectorAll('form').length, 0); assert.match(f.root.textContent, /失败|无效|不可用/);
        f.module.pause(); f.module.resume(); await flush();
        assert.equal(attempts, 2, `${kind}/${failure}`);
        const form = f.root.querySelectorAll('form')[0]; assert.ok(form);
        const input = form.querySelectorAll('input')[0]; input.value = 'unsaved'; form.emit('input');
        f.module.pause(); f.module.resume(); await flush();
        assert.equal(attempts, 2); assert.equal(f.root.querySelectorAll('form')[0], form); assert.equal(input.value, 'unsaved'); f.module.unmount();
    }
});

test('resuming an incomplete integration read does not issue duplicate hydration', async () => {
    let complete; let attempts = 0;
    const f = moduleFixture({ ai: () => { attempts++; return new Promise(resolve => { complete = resolve; }); } });
    f.module.pause(); f.module.resume(); f.module.resume(); assert.equal(attempts, 1);
    complete(ok({ config_version: 1, base_url: '', model_id: 'saved-model' })); await flush();
    f.module.pause(); f.module.resume(); assert.equal(attempts, 1); assert.equal(f.root.querySelectorAll('form').length, 1); f.module.unmount();
});

test('credential replacement hides the entire row until selected and clears text when leaving replacement', async () => {
    for (const source of [false, true]) {
        const f = moduleFixture({
            ai: async () => ok({ config_version: 1, base_url: '', model_id: 'saved-model', credential_configured: true }),
            sources: async () => ok({ sources: [{ provider_id: 'bangumi', config_version: 1, enabled: true, priority: 0, descriptor: { label: 'Bangumi', auth_modes: ['bearer'] }, credential_configured: true }] }),
        }, { source }); await flush();
        const form = f.root.querySelectorAll('form')[0];
        const row = form.children.find(child => child.textContent === '新凭据（仅替换时填写）');
        const value = row.children[0]; const action = form.children.find(child => child.textContent === '凭据操作').children[0];
        assert.equal(row.hidden, true); assert.equal(value.hidden, true);
        action.value = 'replace'; action.emit('change'); assert.equal(row.hidden, false); assert.equal(value.hidden, false);
        value.value = 'synthetic-secret'; action.value = 'clear'; action.emit('change');
        assert.equal(row.hidden, true); assert.equal(value.hidden, true); assert.equal(value.value, '');
        action.value = 'replace'; action.emit('change'); action.value = 'keep'; action.emit('change');
        assert.equal(row.hidden, true); f.module.unmount();
    }
});
