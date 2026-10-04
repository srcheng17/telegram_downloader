import { constants, writeSync } from 'node:fs';
import { open } from 'node:fs/promises';
import { authenticatedContext } from './session_context.mjs';
import { readInput } from './tasks.mjs';
import { readSecret, requireInteractive } from './tty.mjs';
import { CliError, EXIT } from './errors.mjs';

const businessFlags = [
    'id', 'url', 'file', 'output', 'status', 'query', 'page', 'perPage', 'size', 'libraryId',
    'timeout', 'interval', 'force', 'inputFile', 'inputJson', 'provider', 'credential',
    'configVersion', 'model', 'attemptId', 'workspace', 'revision', 'imageId', 'images',
    'clipboard', 'languages', 'direction', 'field', 'candidateId', 'keys', 'resultId',
    'entryId', 'view', 'limit', 'acceptPartial', 'confirmLocked',
];
const opaqueID = /^[A-Za-z0-9_-]{1,128}$/;
const operationID = /^[a-f0-9]{32}$/;
const sha256 = /^[a-f0-9]{64}$/;
const idempotencyKey = /^[A-Za-z0-9_-]{16,128}$/;
const fieldKey = /^[a-z][a-z0-9_.-]{0,127}$/;
const safeCode = /^[a-z][a-z0-9_]{0,63}$/;

function invalid(message) { throw new CliError('invalid_input', message); }
function invalidResponse() { throw new CliError('invalid_response', 'Komga 响应格式无效。', EXIT.transport); }

function checkFlags(options, allowed) {
    for (const key of businessFlags) {
        if (options[key] !== undefined && options[key] !== false && !allowed.includes(key)) invalid('当前 Komga 命令不支持指定的参数。');
    }
}

function validID(value, name, pattern = opaqueID) {
    if (typeof value !== 'string' || !pattern.test(value)) invalid(`请指定有效的${name}。`);
    return value;
}

function intOption(value, name, minimum, maximum, fallback) {
    if (value === undefined) return fallback;
    if (typeof value !== 'string' || !/^(0|[1-9][0-9]*)$/.test(value)) invalid(`${name}无效。`);
    const parsed = Number(value);
    if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) invalid(`${name}超出允许范围。`);
    return parsed;
}

function code(value) { return typeof value === 'string' && safeCode.test(value) ? value : ''; }
function field(value) { return typeof value === 'string' && fieldKey.test(value) ? value : ''; }
function safeWarnings(items) {
    return Array.isArray(items) ? items.slice(0, 64).map((item) => ({ key: field(item?.key), code: code(item?.code) })) : [];
}

function safeConnection(data) {
    if (!data || typeof data.base_url !== 'string' || typeof data.credential_configured !== 'boolean' || !Number.isSafeInteger(data.config_version)) invalidResponse();
    return { target_configured: data.base_url.length > 0, credential_configured: data.credential_configured, config_version: data.config_version };
}

function safeLibrary(item) {
    if (!item || typeof item.id !== 'string' || !opaqueID.test(item.id) || typeof item.writable !== 'boolean') invalidResponse();
    return { id: item.id, writable: item.writable, unavailable: item.unavailable === true };
}

function safeBook(item) {
    if (!item || typeof item.id !== 'string' || !opaqueID.test(item.id) || typeof item.library_id !== 'string' ||
        !opaqueID.test(item.library_id) || typeof item.editable !== 'boolean') invalidResponse();
    return { id: item.id, library_id: item.library_id, editable: item.editable,
        file_type: typeof item.file_type === 'string' && /^\.[A-Za-z0-9]{1,12}$/.test(item.file_type) ? item.file_type : '',
        read_only_reason: code(item.read_only_reason) };
}

function safeEditDetail(data, id) {
    if (!data || data.book?.id !== id || typeof data.can_save !== 'boolean' || !Array.isArray(data.fields) ||
        typeof data.document?.definitions_version !== 'string' || typeof data.source_version !== 'string') invalidResponse();
    return { book: safeBook(data.book), source_version: data.source_version,
        definitions_version: data.document.definitions_version, can_save: data.can_save,
        block_reason: code(data.block_reason), has_comicinfo: data.has_comicinfo === true,
        page_count: Number.isSafeInteger(data.page_count) ? data.page_count : 0,
        page_count_correction_needed: data.page_count_correction_needed === true,
        fields: data.fields.slice(0, 128).map((item) => ({ key: field(item?.key), type: code(item?.type),
            can_set: item?.can_set === true, can_clear: item?.can_clear === true, reason: code(item?.reason) })),
        present_keys: Object.keys(data.document.fields || {}).filter((key) => field(key)).sort(),
        warnings: safeWarnings(data.warnings) };
}

function safePreview(data) {
    if (!data || typeof data.can_save !== 'boolean' || !Array.isArray(data.diffs)) invalidResponse();
    return { preview_token: typeof data.preview_token === 'string' && /^[0-9]{1,16}\.[a-f0-9]{64}\.[a-f0-9]{64}$/.test(data.preview_token) ? data.preview_token : '',
        expires_at: typeof data.expires_at === 'string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d/.test(data.expires_at) ? data.expires_at : '',
        can_save: data.can_save, block_reason: code(data.block_reason), file_changed: data.file_changed === true,
        page_count_correction: data.diffs.some((item) => item?.key === 'page_count' && item?.action === 'corrected'),
        diffs: data.diffs.slice(0, 32).map((item) => ({ key: field(item?.key), action: code(item?.action) })),
        warnings: safeWarnings(data.warnings) };
}

function safeOperation(data, id) {
    if (!data || typeof data.id !== 'string' || !operationID.test(data.id) || id && data.id !== id ||
        typeof data.book_id !== 'string' || !opaqueID.test(data.book_id) || typeof data.state !== 'string' || !code(data.state)) invalidResponse();
    return { id: data.id, book_id: data.book_id, state: code(data.state),
        file_committed: data.file_committed === true, file_no_change: data.file_no_change === true,
        file_restored: data.file_restored === true, projection_applicable: data.projection_applicable === true,
        projection_consistent: data.projection_consistent === true, analyze_verified: data.analyze_verified === true,
        last_error_code: code(data.last_error_code),
        available_actions: Array.isArray(data.available_actions) ? data.available_actions.map(code).filter(Boolean) : [] };
}

function validateChanges(data, saving = false) {
    const required = saving ? ['source_version', 'definitions_version', 'changes', 'preview_token', 'idempotency_key'] : ['source_version', 'definitions_version', 'changes'];
    const optional = ['correct_page_count'];
    if (!data || required.some((key) => !Object.hasOwn(data, key)) || Object.keys(data).some((key) => !required.includes(key) && !optional.includes(key)) ||
        (Object.hasOwn(data, 'correct_page_count') && typeof data.correct_page_count !== 'boolean')) invalid('编辑 JSON 字段不完整或包含未知字段。');
    if (!sha256.test(data.source_version) || typeof data.definitions_version !== 'string' || !data.definitions_version || !Array.isArray(data.changes) || data.changes.length > 32) invalid('编辑版本或变更列表无效。');
    const keys = new Set();
    for (const change of data.changes) {
        if (!change || typeof change !== 'object' || Array.isArray(change) || typeof change.key !== 'string' || !fieldKey.test(change.key) || keys.has(change.key) ||
            !['value', 'cleared'].includes(change.state) ||
            Object.keys(change).some((key) => !['key', 'state', 'value'].includes(key)) ||
            (change.state === 'value' && !Object.hasOwn(change, 'value')) ||
            (change.state === 'cleared' && Object.hasOwn(change, 'value'))) invalid('字段变更格式无效。');
        keys.add(change.key);
    }
    if (saving && (typeof data.preview_token !== 'string' || !data.preview_token || !idempotencyKey.test(data.idempotency_key))) invalid('预览令牌或幂等键无效。');
}

async function requiredInput(options, stdin, saving = false) {
    const input = await readInput(options, stdin);
    if (!input) invalid('请通过 --input-json - 或 --input-file 提供编辑 JSON。');
    validateChanges(input.data, saving);
    return input;
}

async function userTTY(text) {
    let handle;
    try {
        handle = await open('/dev/tty', constants.O_WRONLY | constants.O_NOCTTY);
        writeSync(handle.fd, `${text}\n`);
    } catch {
        throw new CliError('interaction_required', '无法打开独立用户终端。', EXIT.interaction);
    } finally { await handle?.close(); }
}

async function readOperation(client, session, id) {
    const response = await client.request('GET', `/api/komga/edits/${id}`, { cookie: session.cookie });
    return safeOperation(response.data?.operation, id);
}

export async function runKomgaConnection(action, options, { stdin = process.stdin, secretReader = readSecret, reviewer = userTTY,
    interactive = Boolean(process.stdin.isTTY && process.stderr.isTTY) } = {}) {
    if (!['status', 'set', 'test', 'review'].includes(action)) throw new CliError('invalid_command', '未知 Komga 连接命令。');
    checkFlags(options, action === 'set' ? ['credential', 'inputFile', 'inputJson'] : action === 'test' ? ['configVersion'] : []);
    const credential = options.credential || 'keep';
    if (action === 'set' && !['keep', 'clear', 'replace'].includes(credential)) invalid('凭据操作只能是 keep、clear 或 replace。');
    if (action === 'set' && credential === 'replace' || action === 'review') requireInteractive({ json: options.json, interactive });
    const { client, session } = await authenticatedContext(options);
    const request = (method, path, body) => client.request(method, path, { cookie: session.cookie, csrf: session.csrf, ...(body === undefined ? {} : { body }) });
    if (action === 'status') return safeConnection((await request('GET', '/api/settings/komga')).data);
    if (action === 'review') {
        const data = (await request('GET', '/api/settings/komga')).data;
        const summary = safeConnection(data);
        await reviewer(JSON.stringify({ base_url: data.base_url, ...summary }, null, 2));
        return { ...summary, reviewed: true };
    }
    if (action === 'test') {
        const version = intOption(options.configVersion, '配置版本', 0, Number.MAX_SAFE_INTEGER);
        if (version === undefined) invalid('请指定 --config-version。');
        const response = await request('POST', '/api/settings/komga/test', { config_version: version });
        if (response.data?.status !== 'connected' || !Number.isSafeInteger(response.data.allowed_library_count)) invalidResponse();
        return { status: 'connected', allowed_library_count: response.data.allowed_library_count };
    }
    const input = await readInput(options, stdin);
    if (!input || Object.keys(input.data).length !== 2 || !Number.isSafeInteger(input.data.expected_version) || input.data.expected_version < 0 || typeof input.data.base_url !== 'string') invalid('连接 JSON 仅需 expected_version 与 base_url。');
    const value = credential === 'replace' ? await secretReader('Komga API Key') : undefined;
    if (credential === 'replace' && (!value || typeof value !== 'string')) invalid('API Key 不能为空。');
    const response = await request('PUT', '/api/settings/komga', { ...input.data, credential: { action: credential, ...(value ? { value } : {}) } });
    return safeConnection(response.data);
}

export async function runKomga(section, action, options, { stdin = process.stdin, reviewer = userTTY,
    interactive = Boolean(process.stdin.isTTY && process.stderr.isTTY) } = {}) {
    const valid = { libraries: ['list'], books: ['list', 'show', 'edit', 'review'], metadata: ['preview', 'review', 'update'], edits: ['status', 'sync', 'restore'] };
    if (!valid[section]?.includes(action)) throw new CliError('invalid_command', '未知 Komga 作品命令。');
    const allowed = section === 'books' && action === 'list' ? ['libraryId', 'query', 'page', 'size'] :
        section === 'libraries' ? [] :
            section === 'books' ? ['id'] : section === 'metadata' ? ['id', 'inputFile', 'inputJson'] : ['id'];
    checkFlags(options, allowed);
    if (action === 'review') requireInteractive({ json: options.json, interactive });
    const { client, session } = await authenticatedContext(options);
    const read = async (path) => (await client.request('GET', path, { cookie: session.cookie })).data;
    const write = async (path, body) => (await client.request('POST', path, { cookie: session.cookie, csrf: session.csrf, body })).data;
    if (section === 'libraries') {
        const data = await read('/api/komga/libraries');
        if (!Array.isArray(data?.libraries)) invalidResponse();
        return { libraries: data.libraries.map(safeLibrary) };
    }
    if (section === 'books' && action === 'list') {
        const libraryID = validID(options.libraryId, '书库 ID');
        const page = intOption(options.page, '页码', 0, 100_000, 0);
        const size = intOption(options.size, '每页数量', 1, 50, 25);
        if (typeof options.query === 'string' && Buffer.byteLength(options.query) > 256) invalid('查询词超出 256 字节。');
        const params = new URLSearchParams({ library_id: libraryID, query: options.query || '', page: String(page), size: String(size) });
        const data = await read(`/api/komga/books?${params}`);
        if (!data || !Array.isArray(data.books) || !Number.isSafeInteger(data.total_elements) || !Number.isSafeInteger(data.total_pages)) invalidResponse();
        return { page: data.page, size: data.size, total_pages: data.total_pages, total_elements: data.total_elements,
            books: data.books.map(safeBook) };
    }
    if (section === 'books') {
        const id = validID(options.id, '作品 ID');
        if (action === 'show') {
            const data = await read(`/api/komga/books/${id}`);
            return { book: safeBook(data.book || data) };
        }
        const data = await read(`/api/komga/books/${id}/edit`);
        const summary = safeEditDetail(data, id);
        if (action === 'review') {
            await reviewer(JSON.stringify({ book: data.book, document: data.document, fields: data.fields, warnings: data.warnings }, null, 2));
            return { ...summary, reviewed: true };
        }
        return summary;
    }
    if (section === 'metadata') {
        const id = validID(options.id, '作品 ID');
        const input = await requiredInput(options, stdin, action === 'update');
        if (action !== 'update') {
            const data = await write(`/api/komga/books/${id}/preview`, input.data);
            const summary = safePreview(data);
            if (action === 'review') {
                await reviewer(JSON.stringify({ diffs: data.diffs, warnings: data.warnings, can_save: data.can_save,
                    block_reason: data.block_reason, expires_at: data.expires_at }, null, 2));
                return { ...summary, reviewed: true };
            }
            return summary;
        }
        let data;
        try { data = await write(`/api/komga/books/${id}/save`, input.data); }
        catch (error) {
            if (error?.code === 'transport_error') throw new CliError('save_result_unknown', '保存结果不确定；请保留相同幂等键和输入，再查询或重试。', EXIT.transport);
            throw error;
        }
        const operation = safeOperation(data?.operation);
        if (operation.book_id !== id) invalidResponse();
        try { return { operation: await readOperation(client, session, operation.id), verification_pending: false }; }
        catch { return { operation, verification_pending: true }; }
    }
    const id = validID(options.id, '操作 ID', operationID);
    if (action === 'status') return { operation: await readOperation(client, session, id) };
    const before = await readOperation(client, session, id);
    const needed = action === 'sync' ? 'retry_sync' : 'restore';
    if (!before.available_actions.includes(needed)) throw new CliError('edit_conflict', '当前操作不允许此动作，请重新读取状态。', EXIT.conflict);
    let data;
    try { data = await write(`/api/komga/edits/${id}/${action}`, {}); }
    catch (error) {
        if (error?.code === 'transport_error') throw new CliError('operation_result_unknown', '操作结果不确定；请先读取此操作状态。', EXIT.transport);
        throw error;
    }
    const operation = safeOperation(data?.operation, id);
    try { return { operation: await readOperation(client, session, id), verification_pending: false }; }
    catch { return { operation, verification_pending: true }; }
}
