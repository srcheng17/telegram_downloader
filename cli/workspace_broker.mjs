#!/usr/bin/env node
import net from 'node:net';
import { randomBytes } from 'node:crypto';
import { chmod, lstat, mkdir, readdir, unlink } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';
import { authenticatedContext } from './session_context.mjs';
import { createSessionStore } from './session_store.mjs';
import { createWorkspaceState } from './workspace_state.mjs';
import { submitWorkspaceTask } from './tasks.mjs';
import { CliError, EXIT, errorEnvelope } from './errors.mjs';

const ID_PATTERN = /^[a-f0-9]{32}$/u;
const MAX_REQUEST = 1024 * 1024;
const MAX_RESPONSE = 3 * 1024 * 1024;
const IDLE_MS = 30 * 60 * 1000;
const dirDefault = join(homedir(), '.config', 'mediactl', 'workspaces');

function unsafeDirectory() { return new CliError('unsafe_workspace', '工作区运行目录权限不安全。', EXIT.unavailable); }
export async function runtimeDirectory(directory = dirDefault, { create = false } = {}) {
    if (create) await mkdir(directory, { recursive: true, mode: 0o700 });
    let stat;
    try { stat = await lstat(directory); }
    catch (error) { if (error.code === 'ENOENT') return null; throw unsafeDirectory(); }
    if (!stat.isDirectory() || (stat.mode & 0o7777) !== 0o700 || typeof process.getuid === 'function' && stat.uid !== process.getuid()) throw unsafeDirectory();
    return directory;
}

function socketPath(directory, id) {
    if (!ID_PATTERN.test(id || '')) throw new CliError('invalid_input', '工作区 ID 无效。');
    const path = join(directory, `${id}.sock`);
    if (Buffer.byteLength(path) > 100) throw new CliError('workspace_path_too_long', '工作区运行目录路径过长，Unix socket 无法创建。', EXIT.unavailable);
    return path;
}

async function discardStaleSocket(path, expected) {
    try {
        const current = await lstat(path);
        if (current.isSocket() && current.dev === expected.dev && current.ino === expected.ino) await unlink(path);
    } catch { /* another process already removed it */ }
}

export async function cleanupStaleWorkspaces({ directory = dirDefault } = {}) {
    const root = await runtimeDirectory(directory);
    if (!root) return;
    for (const name of await readdir(root)) {
        if (!name.endsWith('.sock') || !ID_PATTERN.test(name.slice(0, -5))) continue;
        const path = socketPath(root, name.slice(0, -5));
        let stat;
        try { stat = await lstat(path); } catch { continue; }
        if (!stat.isSocket() || (stat.mode & 0o7777) !== 0o600) continue;
        await new Promise((resolve) => {
            const socket = net.createConnection(path);
            socket.once('connect', () => { socket.destroy(); resolve(); });
            socket.once('error', async (error) => { if (error.code === 'ECONNREFUSED') await discardStaleSocket(path, stat); resolve(); });
        });
    }
}

export async function workspaceRPC(id, request, { directory = dirDefault, timeoutMs = 150_000 } = {}) {
    const root = await runtimeDirectory(directory);
    if (!root) throw new CliError('workspace_missing', '工作区不存在或已结束。', EXIT.unavailable);
    const path = socketPath(root, id);
    let stat;
    try { stat = await lstat(path); }
    catch { throw new CliError('workspace_missing', '工作区不存在或已结束。', EXIT.unavailable); }
    if (!stat.isSocket() || (stat.mode & 0o7777) !== 0o600 || typeof process.getuid === 'function' && stat.uid !== process.getuid()) throw unsafeDirectory();
    return new Promise((resolve, reject) => {
        const socket = net.createConnection(path); let raw = ''; let settled = false;
        const finish = (error, value) => { if (settled) return; settled = true; socket.destroy(); if (error) reject(error); else resolve(value); };
        socket.setTimeout(timeoutMs, () => finish(new CliError('workspace_timeout', '工作区响应超时。', EXIT.unavailable)));
        socket.once('error', async (error) => {
            if (error.code === 'ECONNREFUSED') await discardStaleSocket(path, stat);
            finish(new CliError('workspace_missing', '工作区不可连接，请重新创建。', EXIT.unavailable));
        });
        socket.on('data', (chunk) => {
            raw += chunk.toString('utf8');
            if (Buffer.byteLength(raw) > MAX_RESPONSE) return finish(new CliError('workspace_response_large', '工作区响应超出限制。', EXIT.unavailable));
            const newline = raw.indexOf('\n'); if (newline < 0) return;
            let value;
            try { value = JSON.parse(raw.slice(0, newline)); }
            catch { return finish(new CliError('workspace_response_invalid', '工作区响应格式无效。', EXIT.transport)); }
            if (value?.ok === true) return finish(null, value.data);
            const error = new CliError(value?.code || 'workspace_error', value?.message || '工作区操作未完成。', value?.exitCode || EXIT.unavailable);
            if (typeof value?.data?.task_id === 'string' && /^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$/u.test(value.data.task_id)) error.taskId = value.data.task_id;
            finish(error);
        });
        socket.once('connect', () => socket.write(`${JSON.stringify(request)}\n`));
        socket.once('end', () => { if (!settled) finish(new CliError('workspace_disconnected', '工作区连接已中断。', EXIT.unavailable)); });
    });
}

export async function startWorkspace(options = {}, { directory = dirDefault, spawnProcess = spawn, store = createSessionStore() } = {}) {
    // Establish authorization before starting a broker. The child rechecks it.
    await authenticatedContext({ ...options, store });
    await runtimeDirectory(directory, { create: true });
    await cleanupStaleWorkspaces({ directory });
    const id = randomBytes(16).toString('hex');
    const script = fileURLToPath(import.meta.url);
    const child = spawnProcess(process.execPath, [script, '--serve', id, directory, options.allowInsecureLoopback ? '1' : '0', store.directory], {
        stdio: 'ignore', detached: true,
    });
    let exited = false;
    child.once('exit', () => { exited = true; });
    child.unref();
    for (let attempt = 0; attempt < 350; attempt++) {
        await new Promise((resolve) => setTimeout(resolve, 100));
        if (exited) break;
        try { return { workspace_id: id, ...(await workspaceRPC(id, { op: 'status' }, { directory, timeoutMs: 2000 })) }; }
        catch (error) { if (error.code !== 'workspace_missing') { child.kill(); throw error; } }
    }
    child.kill();
    throw new CliError('workspace_start_failed', '本机工作区未能启动，请检查 Node 与本机目录。', EXIT.unavailable);
}

export async function closeAllWorkspaces({ directory = dirDefault } = {}) {
    const root = await runtimeDirectory(directory);
    if (!root) return;
    const names = await readdir(root);
    await Promise.allSettled(names.filter((name) => ID_PATTERN.test(name.replace(/\.sock$/u, '')) && name.endsWith('.sock')).map(async (name) => {
        const id = name.slice(0, -5);
        const status = await workspaceRPC(id, { op: 'status' }, { directory, timeoutMs: 2000 });
        await workspaceRPC(id, { op: 'close', expectedRevision: status.revision }, { directory, timeoutMs: 2000 });
    }));
}

export async function serveWorkspace(id, directory, allowInsecureLoopback, configDirectory) {
    const root = await runtimeDirectory(directory);
    if (!root) throw unsafeDirectory();
    const path = socketPath(root, id);
    const store = createSessionStore(configDirectory);
    const context = await authenticatedContext({ store, allowInsecureLoopback });
    const get = async (url) => (await context.client.request('GET', url, { cookie: context.session.cookie })).data;
    const post = async (url, body, timeoutMs = 15_000) => (await context.client.request('POST', url, { body, cookie: context.session.cookie, csrf: context.session.csrf, timeoutMs })).data;
    const schema = await get('/api/metadata/schema');
    let lastActivity = Date.now(); let shuttingDown = false;
    const state = createWorkspaceState({ schema, get, post,
        submit: (action, args, document) => submitWorkspaceTask(action, context, {
            url: args.url, force: args.force === true, file: args.file,
        }, document),
    });
    const server = net.createServer();
    let serial = Promise.resolve();
    async function shutdown() {
        if (shuttingDown) return;
        shuttingDown = true;
        await state.close(); server.close(); await unlink(path).catch(() => {});
    }
    server.on('connection', (socket) => {
        let raw = ''; let used = false;
        socket.setTimeout(150_000, () => socket.destroy());
        socket.on('data', (chunk) => {
            if (used) return;
            raw += chunk.toString('utf8');
            if (Buffer.byteLength(raw) > MAX_REQUEST) { used = true; socket.destroy(); return; }
            const newline = raw.indexOf('\n'); if (newline < 0) return;
            used = true;
            serial = serial.then(async () => {
                let response; let close = false;
                try {
                    const request = JSON.parse(raw.slice(0, newline));
                    if (!request || typeof request.op !== 'string' || typeof request.args !== 'object' && request.args !== undefined) throw new CliError('invalid_input', '工作区请求无效。');
                    const op = request.op;
                    if (op === 'task-upload') socket.setTimeout(11 * 60_000);
                    const reads = new Set(['status', 'review', 'merge', 'provider-list', 'candidate-preview', 'validate', 'document', 'ai-review']);
                    if (!reads.has(op) && request.expectedRevision !== state.revision) throw new CliError('workspace_conflict', '工作区已变化，请重新读取 revision。', EXIT.conflict);
                    lastActivity = Date.now();
                    const data = await state.apply(op, request.args || {}, request.expectedRevision);
                    response = { ok: true, data }; close = op === 'close';
                } catch (error) {
                    const safe = error instanceof CliError ? error : new CliError('workspace_error', '工作区操作未完成；请核对输入或重新读取状态。', EXIT.unavailable);
                    response = { ...errorEnvelope(safe), exitCode: safe.exitCode };
                }
                if (!socket.destroyed) socket.end(`${JSON.stringify(response)}\n`);
                if (close) await shutdown();
            });
        });
    });
    server.listen(path);
    await new Promise((resolve, reject) => { server.once('listening', resolve); server.once('error', reject); });
    await chmod(path, 0o600);
    const idle = setInterval(() => { if (Date.now() - lastActivity >= IDLE_MS) void shutdown(); }, 30_000);
    idle.unref();
    const auth = setInterval(async () => {
        try {
            const saved = await store.load();
            if (!saved || saved.cookie !== context.session.cookie || saved.origin !== context.client.origin) await shutdown();
        } catch { await shutdown(); }
    }, 30_000);
    auth.unref();
    process.once('SIGTERM', () => { void shutdown(); });
    process.once('SIGINT', () => { void shutdown(); });
}

if (process.argv[2] === '--serve') {
    await serveWorkspace(process.argv[3], process.argv[4], process.argv[5] === '1', process.argv[6]).catch(() => { process.exitCode = 1; });
}
