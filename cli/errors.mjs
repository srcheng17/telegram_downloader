export const EXIT = Object.freeze({
    input: 2,
    auth: 3,
    conflict: 4,
    unavailable: 5,
    transport: 6,
    interaction: 7,
});

export class CliError extends Error {
    constructor(code, message, exitCode = EXIT.input) {
        super(message);
        this.name = 'CliError';
        this.code = code;
        this.exitCode = exitCode;
    }
}

const serverCodes = new Set([
    'unauthenticated', 'csrf_rejected', 'rate_limited', 'invalid_password',
    'validation_error', 'invalid_input', 'not_found', 'config_conflict',
    'metadata_conflict', 'rules_conflict', 'search_conflict', 'unavailable',
    'password_invalid', 'attempt_conflict', 'account_changed', 'auth_required',
    'busy', 'telegram_unavailable', 'credential_unavailable', 'invalid_configuration',
    'invalid_rules', 'edit_conflict', 'book_unavailable', 'komga_not_configured',
    'unauthorized', 'forbidden', 'unreachable', 'timeout', 'unsupported_version',
    'redirect_blocked',
    'invalid_response', 'refused', 'schema_unsupported', 'context_exceeded',
    'input_too_large', 'invalid_request', 'disabled', 'not_configured',
    'config_changed', 'cancelled', 'idempotency_conflict', 'submission_source_conflict', 'komga_target_conflict',
]);

const extractionMessages = {
    invalid_response: '返回结果缺少有效证据或不符合格式；当前草稿已保留。',
    refused: '模型拒绝了本次提取；当前草稿已保留。',
    schema_unsupported: '模型不支持本次字段或完整响应校验。',
    context_exceeded: '提取请求超出模型上下文，请减少文字或字段。',
    input_too_large: '提取文字超过允许长度。',
    disabled: 'AI 提取已停用，可继续本地识别或手工填写。',
    not_configured: 'AI 服务尚未配置，可继续本地识别或手工填写。',
    config_changed: '设置已变化，请重新准备提取。',
    cancelled: '本次提取已取消；当前草稿已保留。',
    idempotency_conflict: '提交标识已用于另一份内容，请重新核对提交。',
    submission_source_conflict: '相同来源已有不同内容的任务，请等待完成后重新核对。',
    komga_target_conflict: '书库中已有同名但内容不同的文件，请核对后再交付。',
};

export function httpError(status, body) {
    const provided = typeof body?.code === 'string' && serverCodes.has(body.code) ? body.code : '';
    if (status === 401) return new CliError('unauthenticated', '请先登录或重新登录。', EXIT.auth);
    if (status === 403) return new CliError(provided || 'forbidden', '请求被拒绝，请核对会话与来源。', EXIT.auth);
    if (status === 409) return new CliError(provided || 'conflict', extractionMessages[provided] || '数据已变化，请重新读取后重试。', EXIT.conflict);
    if (status === 429) return new CliError('rate_limited', '请求过于频繁，请稍后重试。', EXIT.unavailable);
    if (status >= 500) return new CliError(provided || 'unavailable', extractionMessages[provided] || '服务暂不可用，请稍后重试。', EXIT.unavailable);
    if (status >= 400) return new CliError(provided || 'invalid_input', extractionMessages[provided] || '请求无效，请核对输入。', EXIT.input);
    return new CliError('unexpected_response', '服务返回了未预期的响应。', EXIT.transport);
}

export function safeError(error) {
    if (error instanceof CliError) return error;
    return new CliError('transport_error', '请求未完成，请检查连接。', EXIT.transport);
}

export function resultEnvelope(result) {
    return { ok: true, code: 'ok', data: result };
}

export function errorEnvelope(error) {
    const safe = safeError(error);
    const envelope = { ok: false, code: safe.code, message: safe.message };
    if (typeof safe.taskId === 'string' && /^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$/.test(safe.taskId)) {
        envelope.data = { task_id: safe.taskId };
    }
    return envelope;
}
