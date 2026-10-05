import { createTasksApi } from '../shared/api/tasks_api.js';

export function createIntegrationsApi(win = window) {
    const api = createTasksApi((url, options) => win.fetch(url, options), { win });
    return {
        changePassword: (input, options) => api.putJson('/api/auth/password', input, options),
        sources: (options) => api.getJson('/api/settings/sources', options),
        saveSource: (id, input, options) => api.putJson(`/api/settings/sources/${encodeURIComponent(id)}`, input, options),
        testSource: (id, version, options) => api.postJson(`/api/settings/sources/${encodeURIComponent(id)}/test`, { config_version: version }, options),
        ai: (options) => api.getJson('/api/settings/ai', options),
        saveAI: (input, options) => api.putJson('/api/settings/ai', input, options),
        models: (version, options) => api.postJson('/api/settings/ai/models', { config_version: version }, options),
        testAI: (version, model, options) => api.postJson('/api/settings/ai/test', { config_version: version, model_id: model }, options),
    };
}

const errorMessages = {
    config_conflict: '设置已在其他请求中更新，请重新进入设置页后重试。',
    invalid_configuration: '设置无效，请检查必填项和凭据操作。更换 AI 地址时需要替换或清除原凭据。',
    credential_unavailable: '服务器暂时无法读取凭据，请检查主密钥配置。',
    unauthorized: '服务拒绝了凭据，请检查授权。', forbidden: '当前凭据没有访问权限。',
    rate_limited: '服务请求过于频繁，请稍后重试。', timeout: '服务请求超时，可稍后重试。',
    unsupported: '服务暂不支持此操作，可手动填写模型 ID。', unavailable: '服务尚不可用，请稍后重试。',
    invalid_response: '服务返回内容未通过验证。', redirect_blocked: '服务要求重定向，已停止请求，请检查已保存的地址。',
    unreachable: '无法连接服务，请检查地址及服务器网络。', response_too_large: '服务响应超出允许范围。',
};
export function integrationError(payload) { return errorMessages[payload?.code] || '操作失败，请稍后重试。'; }

// Each mounted form owns its requests. Editing invalidates all old results,
// including a response from a server which ignored AbortSignal.
export function createRequestScope() {
    let revision = 0;
    let disposed = false;
    const controllers = new Map();
    function invalidate() { revision += 1; for (const c of controllers.values()) c.abort(); controllers.clear(); }
    return {
        invalidate,
        cancel(key) { controllers.get(key)?.abort(); controllers.delete(key); },
        dispose() { disposed = true; invalidate(); },
        start(key) {
            controllers.get(key)?.abort();
            const controller = new AbortController();
            const version = revision;
            controllers.set(key, controller);
            return { signal: controller.signal, current: () => !disposed && !controller.signal.aborted && version === revision && controllers.get(key) === controller };
        },
    };
}
