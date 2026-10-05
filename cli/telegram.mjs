import { authenticatedContext } from './session_context.mjs';
import { readSecret, requireInteractive } from './tty.mjs';
import { openQRTerminal } from './qr_terminal.mjs';
import { CliError, EXIT } from './errors.mjs';

const attemptStates = new Set(['waiting_qr', 'password_required', 'verifying', 'connected', 'cancelled', 'expired', 'failed']);
const accountStates = new Set(['connected', 'auth_required', 'unverified']);
const finalStates = new Set(['connected', 'cancelled', 'expired', 'failed']);

function safeAttempt(raw) {
    if (!raw || !/^[a-f0-9]{32}$/.test(raw.attempt_id || '') || !attemptStates.has(raw.state)) {
        throw new CliError('invalid_response', 'Telegram 登录状态无效。', EXIT.transport);
    }
    return {
        attempt_id: raw.attempt_id, state: raw.state,
        seq: Number.isSafeInteger(raw.seq) ? raw.seq : 0,
        revision: Number.isSafeInteger(raw.revision) ? raw.revision : 0,
        expires_at: raw.expires_at,
        code: typeof raw.code === 'string' && /^[a-z_]{1,50}$/.test(raw.code) ? raw.code : '',
    };
}

function safeAccount(raw) {
    if (!raw || typeof raw.state !== 'string' || !accountStates.has(raw.state)) {
        throw new CliError('invalid_response', 'Telegram 账号状态无效。', EXIT.transport);
    }
    return {
        state: raw.state, revision: Number.isSafeInteger(raw.revision) ? raw.revision : 0,
        busy: raw.busy === true, verified_at: typeof raw.verified_at === 'string' ? raw.verified_at : null,
        max_source_bytes: Number.isSafeInteger(raw.max_source_bytes) ? raw.max_source_bytes : null,
        ...(raw.attempt ? { attempt: safeAttempt(raw.attempt) } : {}),
    };
}

function checkArgs(action, options) {
    if (!['status', 'verify', 'login', 'cancel'].includes(action)) throw new CliError('invalid_command', '未知的 Telegram 连接命令。');
    const forbidden = ['id', 'url', 'file', 'output', 'status', 'query', 'page', 'perPage', 'timeout', 'interval', 'force', 'inputFile', 'inputJson', 'provider', 'credential', 'configVersion', 'model'];
    for (const key of forbidden) {
        if (options[key] !== undefined && options[key] !== false) throw new CliError('invalid_input', 'Telegram 命令不接受该参数。');
    }
    if (action === 'cancel' ? !/^[a-f0-9]{32}$/.test(options.attemptId || '') : options.attemptId !== undefined) {
        throw new CliError('invalid_input', action === 'cancel' ? '请指定有效的登录尝试 ID。' : '当前命令不需要登录尝试 ID。');
    }
}

export async function runTelegram(action, options, {
    secretReader = readSecret, openTerminal = openQRTerminal,
    sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)), now = Date.now,
} = {}) {
    checkArgs(action, options);
    if (action === 'login') requireInteractive({ json: options.json, interactive: options.interactive });
    const { client, session } = await authenticatedContext(options);
    const get = async (path) => (await client.request('GET', path, { cookie: session.cookie })).data;
    const post = async (path, body = {}) => (await client.request('POST', path, { cookie: session.cookie, csrf: session.csrf, body, timeoutMs: 70_000 })).data;

    if (action === 'status') return safeAccount(await get('/api/telegram/account'));
    if (action === 'verify') return safeAccount(await post('/api/telegram/account/verify'));
    if (action === 'cancel') {
        const result = safeAttempt(await post(`/api/telegram/login-attempts/${options.attemptId}/cancel`));
        return { attempt: result, account: safeAccount(await get('/api/telegram/account')) };
    }

    const terminal = await openTerminal();
    let attemptID = '';
    let connected = false;
    let interrupted = false;
    let qrSequence = -1;
    let passwordTries = 0;
    let passwordSequence = -1;
    const interrupt = () => { interrupted = true; };
    process.on('SIGINT', interrupt);
    process.on('SIGTERM', interrupt);
    try {
        let snapshot = await post('/api/telegram/login-attempts');
        attemptID = safeAttempt(snapshot).attempt_id;
        const deadline = now() + 5 * 60_000;
        for (;;) {
            if (interrupted) throw new CliError('cancelled', '登录已取消。', EXIT.interaction);
            const state = safeAttempt(snapshot);
            if (state.attempt_id !== attemptID) throw new CliError('attempt_conflict', '登录尝试已变化。', EXIT.conflict);
            if (state.state === 'connected') {
                connected = true;
                terminal.clear();
                return safeAccount(await post('/api/telegram/account/verify'));
            }
            if (finalStates.has(state.state)) throw new CliError(`telegram_${state.state}`, 'Telegram 登录未完成，请重新发起。', EXIT.unavailable);
            if (now() >= deadline || (Date.parse(state.expires_at) <= now())) throw new CliError('telegram_expired', 'Telegram 登录已超时。', EXIT.unavailable);
            if (state.state === 'waiting_qr' && snapshot.qr && state.seq !== qrSequence && Date.parse(snapshot.qr_expires_at) > now()) {
                terminal.show(snapshot.qr);
                qrSequence = state.seq;
            }
            if (state.state === 'password_required' && state.seq !== passwordSequence) {
                terminal.clear();
                if (state.code === 'password_invalid') terminal.note('两步验证密码无效，请重试。');
                if (++passwordTries > 3) throw new CliError('telegram_password_failed', '两步验证次数已用尽。', EXIT.auth);
                passwordSequence = state.seq;
                const password = await secretReader('Telegram 两步验证密码');
                if (!password) throw new CliError('invalid_input', '两步验证密码不能为空。');
                try {
                    await post(`/api/telegram/login-attempts/${attemptID}/password`, { password });
                    terminal.note('已提交两步验证，正在核验授权…');
                } catch (error) {
                    if (error instanceof CliError && error.code === 'password_invalid') {
                        passwordSequence = -1;
                        terminal.note('两步验证密码无效，请重试。');
                    } else throw error;
                }
            }
            await sleep(1000);
            snapshot = await get(`/api/telegram/login-attempts/${attemptID}`);
        }
    } finally {
        process.off('SIGINT', interrupt);
        process.off('SIGTERM', interrupt);
        if (attemptID && !connected) await post(`/api/telegram/login-attempts/${attemptID}/cancel`).catch(() => {});
        await terminal.close();
    }
}
