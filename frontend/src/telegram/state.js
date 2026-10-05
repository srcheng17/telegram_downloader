const states = new Set(['waiting_qr', 'password_required', 'verifying', 'connected', 'cancelled', 'expired', 'failed']);
export const terminalAttempt = state => ['connected', 'cancelled', 'expired', 'failed'].includes(state);
export const telegramMessages = {
    waiting_qr: '请使用 Telegram 手机客户端扫码，并在手机上确认。',
    password_required: '此账号已启用两步验证，请填写密码。', verifying: '正在关闭登录进程并重新验证授权…',
    connected: '账号已连接，授权已通过独立进程验证。', cancelled: '已取消连接，原有账号保持不变。', expired: '本次连接已过期，请重新扫码。',
    failed: '连接未完成，请重试。', busy: '账号正在处理下载或登录，请等待当前操作结束。',
    auth_required: '尚未连接，或授权已失效。', unverified: '已有账号尚未通过本次运行的授权验证。',
    password_invalid: '两步验证密码不正确，请重新填写。', rate_limited: 'Telegram 暂时限制了请求，请稍后再试。',
    network_error: '暂时无法连接 Telegram，请检查服务器网络后重试。', account_changed: '账号已变化，请重新确认后创建任务。',
    telegram_unavailable: 'Telegram 功能尚未启用，请完成服务器配置。', unavailable: '账号服务暂不可用，请稍后重试。',
    attempt_conflict: '本次连接状态已变化，请刷新状态后重试。', interrupted: '上次连接已中断，请重新扫码。',
};

export function acceptAttempt(current, payload, now = Date.now()) {
    if (!payload || !/^[a-f0-9]{32}$/.test(payload.attempt_id) || !states.has(payload.state)
        || !Number.isSafeInteger(payload.seq) || payload.seq < 1 || !Number.isSafeInteger(payload.revision) || payload.revision < 1
        || !Number.isFinite(Date.parse(payload.expires_at))) return current;
    if (current && (current.attempt_id !== payload.attempt_id || payload.seq <= current.seq || payload.revision <= current.revision)) return current;
    const snapshot = { ...payload, qr: '', qr_expires_at: null };
    if (payload.state === 'waiting_qr' && Date.parse(payload.expires_at) > now && Date.parse(payload.qr_expires_at) > now
        && typeof payload.qr === 'string' && payload.qr.length <= 24000 && /^data:image\/png;base64,[A-Za-z0-9+/=]+$/.test(payload.qr)) {
        snapshot.qr = payload.qr; snapshot.qr_expires_at = payload.qr_expires_at;
    }
    return snapshot;
}
