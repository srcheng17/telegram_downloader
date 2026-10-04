import http from 'node:http';
import https from 'node:https';
import dns from 'node:dns';
import { isIP } from 'node:net';
import { CliError, EXIT, httpError, safeError } from './errors.mjs';

const COOKIE_NAME = 'td_admin_session';
const MAX_RESPONSE_BYTES = 1024 * 1024;
const TIMEOUT_MS = 15_000;

function isLoopback(address) {
    if (address?.startsWith('[') && address.endsWith(']')) address = address.slice(1, -1);
    if (address === '::1') return true;
    if (isIP(address) === 4) {
        const octets = address.split('.').map(Number);
        return octets.length === 4 && octets[0] === 127;
    }
    return false;
}

export function validateServerURL(raw, { allowInsecureLoopback = false } = {}) {
    let url;
    try { url = new URL(raw); } catch { throw new CliError('invalid_server', '服务器地址无效。'); }
    if (url.username || url.password || url.search || url.hash || (url.pathname !== '/' && url.pathname !== '')) {
        throw new CliError('invalid_server', '服务器地址只能填写不含账号、路径或参数的 Origin。');
    }
    if (url.protocol === 'https:') return url.origin;
    if (url.protocol !== 'http:' || !allowInsecureLoopback || !(url.hostname === 'localhost' || isLoopback(url.hostname))) {
        throw new CliError('insecure_server', '认证仅支持 HTTPS；本机 HTTP 需显式允许回环连接。');
    }
    return url.origin;
}

export function parseSessionCookie(headers) {
    const entries = Array.isArray(headers) ? headers : typeof headers === 'string' ? [headers] : [];
    for (const entry of entries) {
        const first = entry.split(';', 1)[0].trim();
        if (!first.startsWith(`${COOKIE_NAME}=`)) continue;
        const value = first.slice(COOKIE_NAME.length + 1);
        if (value === '') return '';
        if (!/^[A-Za-z0-9_-]{43}$/.test(value)) throw new CliError('invalid_session', '服务返回了无效会话。', EXIT.transport);
        return value;
    }
    return undefined;
}

function loopbackLookup(hostname, _options, callback) {
    dns.lookup(hostname, { all: true }, (error, addresses) => {
        if (error) return callback(error);
        const local = addresses.find((entry) => isLoopback(entry.address));
        if (!local || addresses.some((entry) => !isLoopback(entry.address))) {
            return callback(new Error('non-loopback DNS result'));
        }
        callback(null, local.address, local.family);
    });
}

function checkPath(path) {
    if (typeof path !== 'string' || !path.startsWith('/') || path.startsWith('//') || /[\r\n]/.test(path)) {
        throw new CliError('invalid_path', 'API 路径无效。');
    }
    return path;
}

function receiveJSON(response, request) {
    return new Promise((resolve, reject) => {
        const chunks = [];
        let size = 0;
        let ended = false;
        response.on('data', (chunk) => {
            size += chunk.length;
            if (size > MAX_RESPONSE_BYTES) {
                reject(new CliError('response_too_large', '服务响应超出限制。', EXIT.transport));
                request.destroy();
            } else chunks.push(chunk);
        });
        response.once('error', reject);
        response.once('close', () => {
            if (!ended) reject(new CliError('transport_error', '服务连接提前中断。', EXIT.transport));
        });
        response.once('end', () => {
            ended = true;
            let data;
            try { data = JSON.parse(Buffer.concat(chunks).toString('utf8')); }
            catch { return reject(new CliError('invalid_response', '服务响应格式无效。', EXIT.transport)); }
            resolve(data);
        });
    });
}

export function createClient(rawServer, { allowInsecureLoopback = false } = {}) {
    const origin = validateServerURL(rawServer, { allowInsecureLoopback });
    const insecure = origin.startsWith('http:');

    async function ensureWriteContract(cookie, timeoutMs) {
        let data;
        try {
            data = (await request('GET', '/api/client-contract', { cookie, timeoutMs: Math.min(timeoutMs, TIMEOUT_MS) })).data;
        } catch (error) {
            if (error instanceof CliError && ['unauthenticated', 'forbidden'].includes(error.code)) throw error;
            throw new CliError('incompatible_server', '服务端 CLI 协议不可用，请升级并核对版本。', EXIT.unavailable);
        }
        if (!data || data.protocol_version !== 1 || data.client !== 'mediactl') {
            throw new CliError('incompatible_server', '服务端 CLI 协议版本不兼容，请升级并核对版本。', EXIT.unavailable);
        }
    }

    async function stream(method, path, { body, rawBody, input, contentLength, contentType, cookie, csrf, timeoutMs = TIMEOUT_MS } = {}) {
        const normalizedMethod = String(method).toUpperCase();
        if (!['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD'].includes(normalizedMethod)) {
            throw new CliError('invalid_method', 'HTTP 方法无效。');
        }
        const target = new URL(checkPath(path), origin);
        if (target.origin !== origin || target.username || target.password || target.hash) {
            throw new CliError('invalid_path', 'API 路径必须保持同源。');
        }
        if (cookie !== undefined && !/^[A-Za-z0-9_-]{43}$/.test(cookie)) {
            throw new CliError('invalid_session', '本机会话无效。', EXIT.auth);
        }
        const unsafe = !['GET', 'HEAD'].includes(normalizedMethod);
        if (unsafe && !/^[A-Za-z0-9_-]{43}$/.test(csrf || '')) {
            throw new CliError('unauthenticated', '缺少有效会话，请重新登录。', EXIT.auth);
        }
        if ([body !== undefined, rawBody !== undefined, input !== undefined].filter(Boolean).length > 1) {
            throw new CliError('invalid_input', '请求正文只能使用一种输入方式。');
        }
        const payload = body !== undefined ? JSON.stringify(body) : rawBody;
        if (payload !== undefined && typeof payload !== 'string') throw new CliError('invalid_input', '请求正文无效。');
        const headers = { Accept: 'application/json' };
        if (payload !== undefined || input !== undefined) headers['Content-Type'] = contentType || 'application/json';
        if (contentLength !== undefined) {
            if (!Number.isSafeInteger(contentLength) || contentLength < 0) throw new CliError('invalid_input', '请求长度无效。');
            headers['Content-Length'] = String(contentLength);
        }
        if (cookie) headers.Cookie = `${COOKIE_NAME}=${cookie}`;
        if (unsafe) {
            headers.Origin = origin;
            headers['X-CSRF-Token'] = csrf;
        }
        // A fresh, read-only contract check fences every business mutation.
        // Login cannot call the protected endpoint; logout must remain usable
        // to revoke sessions on an older server.
        if (unsafe && !['/api/auth/login', '/api/auth/logout'].includes(target.pathname)) {
            try { await ensureWriteContract(cookie, timeoutMs); }
            catch (error) { input?.destroy(); throw error; }
        }
        const transport = insecure ? http : https;

        try {
            const response = await new Promise((resolve, reject) => {
                const req = transport.request(target, {
                    method: normalizedMethod, headers, agent: false,
                    ...(insecure ? { lookup: loopbackLookup } : {}),
                }, (res) => {
                    if (res.statusCode >= 300 && res.statusCode < 400) {
                        res.resume();
                        input?.destroy();
                        return reject(new CliError('unsafe_redirect', '服务返回重定向，认证信息未转发。', EXIT.transport));
                    }
                    resolve({ status: res.statusCode, headers: res.headers, body: res, request: req });
                });
                req.setTimeout(timeoutMs, () => req.destroy(new Error('timeout')));
                req.once('error', reject);
                req.once('socket', (socket) => {
                    const send = () => {
                        if (insecure && !isLoopback(socket.remoteAddress)) {
                            req.destroy(new Error('non-loopback connection'));
                            return;
                        }
                        if (input) {
                            input.once('error', (error) => req.destroy(error));
                            input.pipe(req);
                        } else req.end(payload);
                    };
                    if (socket.connecting) socket.once(insecure ? 'connect' : 'secureConnect', send);
                    else send();
                });
            });
            if (response.status < 200 || response.status >= 300) {
                input?.destroy();
                let errorBody = null;
                if (normalizedMethod !== 'HEAD') errorBody = await receiveJSON(response.body, response.request).catch(() => null);
                else response.body.resume();
                throw httpError(response.status, errorBody);
            }
            return response;
        } catch (error) {
            input?.destroy();
            throw safeError(error);
        }
    }

    async function request(method, path, options = {}) {
        const response = await stream(method, path, options);
        let data;
        try {
            data = String(method).toUpperCase() === 'HEAD' ? (response.body.resume(), null) : await receiveJSON(response.body, response.request);
        } catch (error) { throw safeError(error); }
        return { status: response.status, data, cookie: parseSessionCookie(response.headers['set-cookie']) };
    }

    return { origin, request, stream };
}
