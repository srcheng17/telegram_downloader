import { constants, createReadStream } from 'node:fs';
import { open } from 'node:fs/promises';
import { basename, extname } from 'node:path';
import { authenticatedContext } from './session_context.mjs';
import { safeSummary, safeTask, terminal } from './task_projection.mjs';
import { saveArtifact } from './artifact.mjs';
import { CliError, EXIT } from './errors.mjs';

const MAX_INPUT_BYTES = 1024 * 1024;
const MAX_UPLOAD_BYTES = 64 * 1024 * 1024;
const sourceFields = new Set(['url', 'force', 'kind', 'metadata_document', 'author', 'series_name', 'series_number', 'comic_name', 'summary', 'tags', 'genres']);
const uploadFields = new Set(['file_name', 'file_size', 'metadata_document', 'author', 'series_name', 'series_number', 'comic_name', 'summary', 'tags', 'genres']);
const validStatuses = new Set(['CREATED', 'READY', 'RUNNING', 'CANCELING', 'SUCCEEDED', 'FAILED', 'CANCELED']);

function validID(id) {
    if (typeof id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$/.test(id)) throw new CliError('invalid_input', '任务 ID 无效。');
    return id;
}

function integerOption(raw, label, minimum, maximum, fallback) {
    if (raw === undefined) return fallback;
    const number = Number(raw);
    if (!Number.isSafeInteger(number) || number < minimum || number > maximum) throw new CliError('invalid_input', `${label}超出允许范围。`);
    return number;
}

export async function readInput(options, stdin = process.stdin) {
    if (options.inputFile && options.inputJson) throw new CliError('invalid_input', '只能选择一种 JSON 输入。');
    if (!options.inputFile && !options.inputJson) return null;
    let raw;
    if (options.inputJson) {
        if (options.inputJson !== '-') throw new CliError('invalid_input', '--input-json 只支持标准输入 -。');
        const chunks = [];
        let size = 0;
        for await (const chunk of stdin) {
            const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
            size += bytes.length;
            if (size > MAX_INPUT_BYTES) throw new CliError('invalid_input', 'JSON 输入超出 1 MiB。');
            chunks.push(bytes);
        }
        raw = Buffer.concat(chunks).toString('utf8');
    } else {
        let handle;
        try {
            handle = await open(options.inputFile, constants.O_RDONLY | constants.O_NOFOLLOW);
            const stat = await handle.stat();
            if (!stat.isFile() || stat.size > MAX_INPUT_BYTES) throw new CliError('invalid_input', 'JSON 输入文件无效或超出 1 MiB。');
            raw = await handle.readFile({ encoding: 'utf8' });
        } catch (error) {
            if (error instanceof CliError) throw error;
            throw new CliError('invalid_input', '无法读取 JSON 输入文件。');
        } finally { await handle?.close(); }
    }
    let data;
    try { data = JSON.parse(raw); }
    catch { throw new CliError('invalid_input', 'JSON 输入格式无效。'); }
    if (!data || typeof data !== 'object' || Array.isArray(data)) throw new CliError('invalid_input', 'JSON 输入必须是对象。');
    return { raw, data };
}

function checkFields(data, allowed) {
    if (Object.keys(data).some((key) => !allowed.has(key))) throw new CliError('invalid_input', 'JSON 输入包含不支持的字段。');
}

function forceValue(value) {
    return value === true || value === 'true' || value === 1;
}

async function getTask(context, id) {
    const response = await context.client.request('GET', `/api/tasks/${encodeURIComponent(validID(id))}`, { cookie: context.session.cookie });
    return safeTask(response.data?.task || response.data);
}

async function listTasks(context, options) {
    const page = integerOption(options.page, '页码', 1, 1_000_000, 1);
    const perPage = integerOption(options.perPage, '每页数量', 1, 100, 25);
    const status = options.status ? String(options.status).toUpperCase() : '';
    if (status && !validStatuses.has(status)) throw new CliError('invalid_input', '任务状态筛选无效。');
    const query = options.query || '';
    if (Buffer.byteLength(query) > 120) throw new CliError('invalid_input', '关键词不得超过 120 字节。');
    const params = new URLSearchParams({ page: String(page), per_page: String(perPage) });
    if (status) params.set('status', status);
    if (query) params.set('q', query);
    const response = await context.client.request('GET', `/api/logs?${params}`, { cookie: context.session.cookie });
    const data = response.data;
    if (!data || !Array.isArray(data.logs)) throw new CliError('invalid_response', '任务列表响应无效。', EXIT.transport);
    return {
        total: Number.isSafeInteger(data.total) ? data.total : 0,
        page: Number.isSafeInteger(data.page) ? data.page : page,
        per_page: Number.isSafeInteger(data.per_page) ? data.per_page : perPage,
        total_pages: Number.isSafeInteger(data.total_pages) ? data.total_pages : 1,
        has_active_tasks: data.has_active_tasks === true,
        tasks: data.logs.map(safeTask),
    };
}

async function createURL(context, options, stdin, document) {
    const input = document === undefined ? await readInput(options, stdin) : null;
    if (input && options.url) throw new CliError('invalid_input', '--url 与 JSON 输入不可同时使用。');
    let body;
    if (document !== undefined) {
        if (typeof options.url !== 'string' || !options.url.trim()) throw new CliError('invalid_input', '请提供 Telegraph 链接。');
        body = { body: { url: options.url, force: options.force === true, metadata_document: document } };
    } else if (input) {
        checkFields(input.data, sourceFields);
        if (typeof input.data.url !== 'string' || !input.data.url.trim()) throw new CliError('invalid_input', '请提供 Telegraph 链接。');
        if (forceValue(input.data.force) !== Boolean(options.force)) {
            throw new CliError('invalid_input', '重抓需要 --force 与 JSON 中的 force=true 同时明确指定。');
        }
        body = { rawBody: input.raw };
    } else {
        if (typeof options.url !== 'string' || !options.url.trim()) throw new CliError('invalid_input', '请提供 Telegraph 链接。');
        body = { body: { url: options.url, force: Boolean(options.force) } };
    }
    const response = await context.client.request('POST', '/download', { cookie: context.session.cookie, csrf: context.session.csrf, ...body });
    const data = response.data;
    if (data?.ok !== true || !data.task) throw new CliError('invalid_response', '创建任务响应无效。', EXIT.transport);
    return {
        task: safeTask(data.task),
        created: data.duplicate !== true,
        duplicate: data.duplicate === true,
        active: data.active === true,
        needs_confirmation: data.needs_confirmation === true,
    };
}

async function upload(context, options, stdin, document) {
    if (typeof options.file !== 'string' || !options.file) throw new CliError('invalid_input', '请用 --file 指定本地压缩包。');
    const name = basename(options.file);
    if (!['.zip', '.cbz', '.rar', '.7z'].includes(extname(name).toLowerCase())) throw new CliError('invalid_input', '只支持 ZIP、CBZ、RAR 或 7Z。');
    let handle;
    try {
        handle = await open(options.file, constants.O_RDONLY | constants.O_NOFOLLOW);
        const stat = await handle.stat();
        if (!stat.isFile() || stat.size <= 0 || stat.size > MAX_UPLOAD_BYTES) throw new CliError('invalid_input', '压缩包大小必须在 1 字节到 64 MiB 之间。');
        const input = document === undefined ? await readInput(options, stdin) : null;
        let body;
        if (document !== undefined) body = { body: { file_name: name, file_size: stat.size, metadata_document: document } };
        else if (input) {
            checkFields(input.data, uploadFields);
            if (input.data.file_name !== name || input.data.file_size !== stat.size) {
                throw new CliError('invalid_input', 'JSON 中的文件名和大小必须与本地压缩包一致。');
            }
            body = { rawBody: input.raw };
        } else body = { body: { file_name: name, file_size: stat.size } };
        const init = await context.client.request('POST', '/api/tasks/upload/init', {
            cookie: context.session.cookie, csrf: context.session.csrf, ...body,
        });
        const taskID = validID(init.data?.task_id);
        const uploadURL = init.data?.upload_url;
        if (init.data?.ok !== true || typeof uploadURL !== 'string') throw new CliError('invalid_response', '上传初始化响应无效。', EXIT.transport);
        const source = createReadStream(null, { fd: handle.fd, autoClose: false, start: 0, end: stat.size - 1 });
        try {
            const attached = await context.client.request('PUT', uploadURL, {
                cookie: context.session.cookie, csrf: context.session.csrf,
                input: source, contentLength: stat.size, contentType: 'application/octet-stream', timeoutMs: 10 * 60_000,
            });
            if (attached.data?.ok !== true || attached.data?.task_id !== taskID) {
                throw new CliError('invalid_response', '上传附着响应无效。', EXIT.transport);
            }
            const confirmed = await getTask(context, taskID);
            if (confirmed.status === 'CREATED') throw new CliError('upload_incomplete', '文件已发送，但任务仍等待来源；请核对后重试。', EXIT.unavailable);
            return { task: confirmed, upload_complete: true };
        } catch (error) {
            const incomplete = new CliError('upload_incomplete', '任务已创建，但文件上传或回读未完成。', EXIT.unavailable);
            incomplete.taskId = taskID;
            incomplete.reasonCode = error instanceof CliError ? error.code : 'transport_error';
            throw incomplete;
        } finally { if (!source.readableEnded) source.destroy(); }
    } catch (error) {
        if (error instanceof CliError) throw error;
        throw new CliError('invalid_input', '无法读取本地压缩包。');
    } finally {
        await handle?.close().catch((error) => { if (error.code !== 'EBADF') throw error; });
    }
}

async function checkedAction(context, id, action) {
    const task = await getTask(context, id);
    if (!task.available_actions.includes(action)) throw new CliError('action_unavailable', '当前任务不允许此操作，请重新读取状态。', EXIT.conflict);
    return task;
}

const taskOptions = {
    list: ['page', 'perPage', 'status', 'query'],
    show: ['id'], wait: ['id', 'timeout', 'interval'],
    cancel: ['id'], retry: ['id'],
    'artifact-check': ['id'], 'artifact-get': ['id', 'output'],
    'copy-to-komga': ['id'], 'create-url': ['url', 'force', 'inputFile', 'inputJson'],
    upload: ['file', 'inputFile', 'inputJson'],
};
const businessFlags = ['id', 'url', 'file', 'output', 'status', 'query', 'page', 'perPage', 'timeout', 'interval', 'force', 'inputFile', 'inputJson', 'provider', 'credential', 'configVersion', 'model', 'attemptId'];

function checkOptions(action, options) {
    const allowed = taskOptions[action];
    if (!allowed) throw new CliError('invalid_command', '未知的任务命令。');
    for (const key of businessFlags) {
        if (options[key] !== undefined && options[key] !== false && !allowed.includes(key)) {
            throw new CliError('invalid_input', '当前命令不支持指定的参数。');
        }
    }
}

export async function runOverview(options) {
    for (const key of businessFlags) {
        if (options[key] !== undefined && options[key] !== false) throw new CliError('invalid_input', '概览命令不支持任务参数。');
    }
    const context = await authenticatedContext(options);
    const response = await context.client.request('GET', '/api/summary', { cookie: context.session.cookie });
    return safeSummary(response.data);
}

export async function runTasks(action, options, { stdin = process.stdin, sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)), now = Date.now } = {}) {
    checkOptions(action, options);
    const context = await authenticatedContext(options);
    if (action === 'list') return listTasks(context, options);
    if (action === 'create-url') return createURL(context, options, stdin);
    if (action === 'upload') return upload(context, options, stdin);
    const id = validID(options.id);
    if (action === 'show') return { task: await getTask(context, id) };
    if (action === 'wait') {
        const timeout = integerOption(options.timeout, '等待时间', 1, 3600, 300);
        const interval = integerOption(options.interval, '轮询间隔', 1, 60, 2);
        const deadline = now() + timeout * 1000;
        for (;;) {
            const task = await getTask(context, id);
            if (terminal(task.status)) return { task, final: true };
            if (now() >= deadline) {
                const error = new CliError('wait_timeout', '等待超时，任务仍在进行。', EXIT.unavailable);
                error.taskId = id;
                throw error;
            }
            await sleep(Math.min(interval * 1000, Math.max(0, deadline - now())));
        }
    }
    if (action === 'cancel' || action === 'retry') {
        await checkedAction(context, id, action);
        const response = await context.client.request('POST', `/api/tasks/${encodeURIComponent(id)}/${action}`, {
            cookie: context.session.cookie, csrf: context.session.csrf,
        });
        if (response.data?.ok !== true) throw new CliError('invalid_response', '任务操作响应无效。', EXIT.transport);
        const task = await getTask(context, id);
        return { requested: true, task, final: terminal(task.status), ...(action === 'cancel' ? { cancelled: task.status === 'CANCELED' } : {}) };
    }
    if (action === 'artifact-check' || action === 'artifact-get') {
        await checkedAction(context, id, 'download');
        await context.client.request('HEAD', `/api/tasks/${encodeURIComponent(id)}/download`, { cookie: context.session.cookie });
        if (action === 'artifact-check') return { task_id: id, available: true };
        if (!options.output) throw new CliError('invalid_input', '请用 --output 指定本地保存路径。');
        const response = await context.client.stream('GET', `/api/tasks/${encodeURIComponent(id)}/download`, {
            cookie: context.session.cookie, timeoutMs: 10 * 60_000,
        });
        return { task_id: id, ...await saveArtifact(response, options.output) };
    }
    if (action === 'copy-to-komga') {
        await checkedAction(context, id, 'copy_to_komga');
        const response = await context.client.request('POST', `/api/tasks/${encodeURIComponent(id)}/copy-to-komga`, {
            cookie: context.session.cookie, csrf: context.session.csrf,
        });
        if (response.data?.ok !== true || response.data?.task_id !== id) throw new CliError('invalid_response', '复制结果响应无效。', EXIT.transport);
        return { task: await getTask(context, id), copy_completed: true, komga_indexed: 'unverified' };
    }
    throw new CliError('invalid_command', '未知的任务命令。');
}

// The private workspace broker passes its validated in-memory document here.
// It uses the same task submission and upload-completeness paths as runTasks.
export async function submitWorkspaceTask(action, context, options, document) {
    if (action === 'create-url') return createURL(context, options, null, document);
    if (action === 'upload') return upload(context, options, null, document);
    throw new CliError('invalid_command', '未知的工作区任务命令。');
}
