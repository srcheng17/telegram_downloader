import { createClient } from './http.mjs';
import { CliError, EXIT } from './errors.mjs';

export async function authenticatedContext({ store, server, allowInsecureLoopback = false }) {
    const saved = await store.load();
    if (!saved) throw new CliError('unauthenticated', '请先在独立终端登录。', EXIT.auth);
    const client = createClient(server || saved.origin, { allowInsecureLoopback });
    if (client.origin !== saved.origin) throw new CliError('origin_mismatch', '当前会话属于其他服务器。', EXIT.auth);
    const response = await client.request('GET', '/api/auth/session', { cookie: saved.cookie });
    const state = response.data;
    if (!state || typeof state.authenticated !== 'boolean' || !/^[A-Za-z0-9_-]{43}$/.test(state.csrf_token || '')) {
        throw new CliError('invalid_response', '服务返回了无效会话。', EXIT.transport);
    }
    if (!state.authenticated) {
        await store.clear();
        throw new CliError('unauthenticated', '会话已失效，请重新登录。', EXIT.auth);
    }
    const session = { ...saved, csrf: state.csrf_token, expiresAt: state.expires_at };
    if (session.csrf !== saved.csrf || session.expiresAt !== saved.expiresAt) await store.save(session);
    return { client, session };
}
