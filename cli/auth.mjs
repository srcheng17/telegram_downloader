import { createClient } from './http.mjs';
import { CliError, EXIT } from './errors.mjs';
import { readSecret, requireInteractive } from './tty.mjs';

function authSession(payload) {
    if (typeof payload?.authenticated !== 'boolean' || !/^[A-Za-z0-9_-]{43}$/.test(payload.csrf_token || '')) {
        throw new CliError('invalid_response', '服务返回了无效会话。', EXIT.transport);
    }
    return payload;
}

function authenticatedResponse(response) {
    const payload = authSession(response.data);
    if (!payload.authenticated) throw new CliError('unauthenticated', '请先登录或重新登录。', EXIT.auth);
    return payload;
}

function serverFor(server, saved, allowInsecureLoopback) {
    const raw = server || saved?.origin;
    if (!raw) throw new CliError('server_required', '请用 --server 指定工作台地址。');
    const client = createClient(raw, { allowInsecureLoopback });
    if (saved && server && client.origin !== saved.origin) {
        throw new CliError('origin_mismatch', '当前会话属于其他服务器，请先退出或单独登录。', EXIT.auth);
    }
    return client;
}

export async function runAuth(action, {
    server, allowInsecureLoopback = false, json = false, store,
    interactive = Boolean(process.stdin.isTTY && process.stderr.isTTY),
    secretReader = readSecret,
    ...extra
} = {}) {
    if (!store) throw new CliError('invalid_state', '会话存储不可用。', EXIT.unavailable);
    for (const key of ['id', 'url', 'file', 'output', 'status', 'query', 'page', 'perPage', 'timeout', 'interval', 'force', 'inputFile', 'inputJson', 'provider', 'credential', 'configVersion', 'model', 'attemptId']) {
        if (extra[key] !== undefined && extra[key] !== false) throw new CliError('invalid_input', '认证命令不接受任务参数或秘密文件。');
    }

    if (action === 'login') {
        requireInteractive({ json, interactive });
        const saved = await store.load();
        const client = serverFor(server, server ? null : saved, allowInsecureLoopback);
        const password = await secretReader('管理员密码');
        if (!password) throw new CliError('invalid_input', '密码不能为空。');
        const pre = await client.request('GET', '/api/auth/session');
        const prePayload = authSession(pre.data);
        if (!pre.cookie || prePayload.authenticated) {
            throw new CliError('invalid_response', '服务未提供预登录会话。', EXIT.transport);
        }
        const logged = await client.request('POST', '/api/auth/login', {
            body: { password }, cookie: pre.cookie, csrf: prePayload.csrf_token,
        });
        authenticatedResponse(logged);
        if (!logged.cookie || logged.cookie === pre.cookie) {
            throw new CliError('invalid_response', '登录后会话未轮换。', EXIT.transport);
        }
        const verified = await client.request('GET', '/api/auth/session', { cookie: logged.cookie });
        const state = authenticatedResponse(verified);
        await store.save({ origin: client.origin, cookie: logged.cookie, csrf: state.csrf_token, expiresAt: state.expires_at });
        return { authenticated: true, expires_at: state.expires_at };
    }

    if (action === 'status') {
        const saved = await store.load();
        if (!saved) return { authenticated: false };
        const client = serverFor(server, saved, allowInsecureLoopback);
        const response = await client.request('GET', '/api/auth/session', { cookie: saved.cookie });
        const state = authSession(response.data);
        if (!state.authenticated) {
            await store.clear();
            return { authenticated: false };
        }
        if (state.csrf_token !== saved.csrf || state.expires_at !== saved.expiresAt) {
            await store.save({ ...saved, csrf: state.csrf_token, expiresAt: state.expires_at });
        }
        return { authenticated: true, expires_at: state.expires_at };
    }

    if (action === 'logout') {
        const saved = await store.load();
        if (!saved) return { authenticated: false };
        const client = serverFor(server, saved, allowInsecureLoopback);
        const check = await client.request('GET', '/api/auth/session', { cookie: saved.cookie });
        const state = authSession(check.data);
        if (state.authenticated) {
            await client.request('POST', '/api/auth/logout', { cookie: saved.cookie, csrf: state.csrf_token });
        }
        await store.clear();
        return { authenticated: false };
    }

    if (action === 'password-change') {
        requireInteractive({ json, interactive });
        const saved = await store.load();
        if (!saved) throw new CliError('unauthenticated', '请先登录。', EXIT.auth);
        const client = serverFor(server, saved, allowInsecureLoopback);
        const check = await client.request('GET', '/api/auth/session', { cookie: saved.cookie });
        const state = authenticatedResponse(check);
        const current = await secretReader('当前密码');
        const next = await secretReader('新密码');
        const confirm = await secretReader('再次输入新密码');
        if (next !== confirm) throw new CliError('invalid_input', '两次新密码不一致。');
        if ([...next].length < 12 || Buffer.byteLength(next) > 72) {
            throw new CliError('invalid_password', '新密码至少 12 个字符，且不能超过 72 字节。');
        }
        await client.request('PUT', '/api/auth/password', {
            cookie: saved.cookie, csrf: state.csrf_token,
            body: { current_password: current, new_password: next },
        });
        await store.clear();
        return { authenticated: false, password_changed: true };
    }

    throw new CliError('invalid_command', '未知的认证命令。');
}
