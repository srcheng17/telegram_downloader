import { constants, writeSync } from 'node:fs';
import { open, unlink } from 'node:fs/promises';
import { isAbsolute } from 'node:path';
import { authenticatedContext } from './session_context.mjs';
import { startWorkspace, workspaceRPC } from './workspace_broker.mjs';
import { readInput } from './tasks.mjs';
import { readSecret, requireInteractive } from './tty.mjs';
import { CliError, EXIT, safeError } from './errors.mjs';

function invalid(message) { throw new CliError('invalid_input', message); }
function intValue(value, name) {
    const result = Number(value);
    if (!Number.isSafeInteger(result) || result < 0) invalid(`${name}必须是非负整数。`);
    return result;
}
function list(value, name) {
    if (typeof value !== 'string' || !value.trim()) invalid(`请指定${name}。`);
    const rows = value.split(',').map((item) => item.trim());
    if (rows.some((item) => !item) || new Set(rows).size !== rows.length) invalid(`${name}列表无效。`);
    return rows;
}
function required(value, name) { if (typeof value !== 'string' || !value) invalid(`请指定${name}。`); return value; }
async function input(options, stdin, fields) {
    const result = await readInput(options, stdin);
    if (!result || Object.keys(result.data).some((key) => !fields.includes(key)) || fields.some((key) => !Object.hasOwn(result.data, key))) invalid(`请用结构化 JSON 提供 ${fields.join('、')}。`);
    return result.data;
}

async function ttyDecision(view, { interactive = Boolean(process.stdin.isTTY && process.stderr.isTTY) } = {}) {
    requireInteractive({ interactive });
    await printTTY(`${view}\n确认发送上述文字到已保存的 AI 目标？`, interactive);
    return await readSecret('输入 YES 后继续') === 'YES';
}

function humanReview(data) {
    const lines = [`工作区修订：${data.revision}`, `文档修订：${data.document_revision}`];
    for (const image of data.images || []) lines.push(`\n图片 ${image.image_id} (${image.status})\nOCR 原文：\n${image.raw_text}\n校对文字：\n${image.edited_text}`);
    lines.push(`\n合并文字：${data.merged?.warning_codes?.includes('merge_unavailable') ? '超出 64 KiB，请逐图缩短校对文字。' : ''}\n${data.merged?.text || ''}`);
    lines.push(`\n元数据草稿：\n${JSON.stringify(data.document, null, 2)}`);
    for (const result of data.results || []) lines.push(`\n书目结果 ${result.result_id}（${result.provider_id}）：\n${JSON.stringify(result.record, null, 2)}`);
    for (const entry of data.history || []) lines.push(`\n历史记录 ${entry.entry_id}：\n${JSON.stringify(entry, null, 2)}`);
    for (const candidate of data.candidates || []) lines.push(`\n候选 ${candidate.candidate_id}：\n${JSON.stringify(candidate.fields, null, 2)}`);
    if (data.send) lines.push(`\nAI 发送预览：\n目标：${data.send.target || '未设置'}\n模型：${data.send.model_id}\n字段：${data.send.field_keys.join('、')}\n文字：\n${data.send.text}`);
    return lines.join('\n');
}

async function printTTY(value, interactive) {
    requireInteractive({ interactive });
    let handle;
    try { handle = await open('/dev/tty', constants.O_WRONLY | constants.O_NOCTTY); writeSync(handle.fd, `${value}\n`); }
    catch { throw new CliError('interaction_required', '无法打开独立用户终端。', EXIT.interaction); }
    finally { await handle?.close(); }
}

async function exportDocument(id, output) {
    if (!isAbsolute(output || '')) invalid('导出路径必须是绝对路径。');
    const result = await workspaceRPC(id, { op: 'document' });
    let handle; let created = false; let success = false;
    try {
        handle = await open(output, constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW, 0o600);
        created = true;
        const bytes = Buffer.from(`${JSON.stringify(result.document)}\n`);
        await handle.writeFile(bytes); await handle.sync();
        success = true;
        return { exported: true, bytes: bytes.length, document_revision: result.document.revision };
    } catch {
        throw new CliError('export_failed', '导出失败；目标文件已存在或目录不可写。', EXIT.unavailable);
    } finally { await handle?.close().catch(() => {}); if (created && !success) await unlink(output).catch(() => {}); }
}

export async function runWorkspace(action, subaction, options, { stdin = process.stdin, interactive = Boolean(process.stdin.isTTY && process.stderr.isTTY), starter = startWorkspace, rpc = workspaceRPC } = {}) {
    if (action === 'start' && !subaction) return starter(options, { store: options.store });
    const id = required(options.workspace, '工作区 ID');
    if (action === 'status' && !subaction) return rpc(id, { op: 'status' });
    if (action === 'review' && !subaction) {
        if (options.json) throw new CliError('interaction_required', '详细核对仅在独立用户终端显示。', EXIT.interaction);
        const review = await rpc(id, { op: 'review' });
        await printTTY(humanReview(review), interactive);
        return { revision: review.revision, reviewed: true, image_count: review.images.length, candidate_count: review.candidates.length };
    }
    if (action === 'export' && !subaction) return exportDocument(id, required(options.output, '导出绝对路径'));
    let op; let args = {};
    if (action === 'stop' && !subaction) op = 'close';
    else if (action === 'validate' && !subaction) op = 'validate';
    else if (action === 'merge' && !subaction) { op = 'merge'; args.acceptPartial = options.acceptPartial === true; }
    else if (action === 'image') {
        if (subaction === 'add') {
            op = 'image-add';
            if (options.inputFile || options.inputJson) {
                if (options.images?.length || options.clipboard) invalid('图片 JSON 输入不能与 --image 或 --clipboard 混用。');
                const value = await readInput(options, stdin);
                if (!value || Object.keys(value.data).some((key) => !['images', 'clipboard'].includes(key)) || !Array.isArray(value.data.images) || value.data.images.some((path) => typeof path !== 'string') || value.data.clipboard !== undefined && typeof value.data.clipboard !== 'boolean') invalid('图片 JSON 需要 images 路径数组与可选 clipboard。');
                args = { paths: value.data.images, clipboard: value.data.clipboard === true };
            } else args = { paths: options.images || [], clipboard: options.clipboard === true };
        }
        else if (subaction === 'languages') { op = 'image-languages'; args.languages = list(options.languages, '语言'); }
        else if (subaction === 'edit') { op = 'image-edit'; args = { imageId: required(options.imageId, '图片 ID'), ...(await input(options, stdin, ['text'])) }; }
        else if (subaction === 'accept-raw') { op = 'image-accept-raw'; args.imageId = required(options.imageId, '图片 ID'); }
        else if (subaction === 'retry') { op = 'image-retry'; args.imageId = required(options.imageId, '图片 ID'); }
        else if (subaction === 'remove') { op = 'image-remove'; args.imageId = required(options.imageId, '图片 ID'); }
        else if (subaction === 'move') { op = 'image-move'; args = { imageId: required(options.imageId, '图片 ID'), direction: required(options.direction, '方向') }; }
        else if (subaction === 'cancel') op = 'image-cancel';
    } else if (action === 'text' && subaction === 'set') { op = 'text-set'; args = await input(options, stdin, ['text']); }
    else if (action === 'field') {
        args.key = required(options.field, '字段 key');
        if (subaction === 'set') { op = 'field-set'; Object.assign(args, await input(options, stdin, ['value'])); }
        else if (subaction === 'clear') op = 'field-clear';
        else if (subaction === 'unlock') op = 'field-unlock';
    } else if (action === 'rules' && subaction === 'preview') { op = 'rules-preview'; args.acceptPartial = options.acceptPartial === true; }
    else if (action === 'provider') {
        if (subaction === 'list') op = 'provider-list';
        else if (subaction === 'search') { op = 'provider-search'; const value = await input(options, stdin, ['keyword', 'provider_ids']); args = { keyword: value.keyword, providerIds: value.provider_ids }; }
        else if (subaction === 'resolve') { op = 'provider-resolve'; args = { resultId: required(options.resultId, '搜索结果 ID'), customMappings: {} }; }
    } else if (action === 'history') {
        if (subaction === 'list') { op = 'history-list'; args.limit = options.limit === undefined ? 20 : intValue(options.limit, '历史条数'); }
        else if (subaction === 'select') { op = 'history-select'; args = { entryId: required(options.entryId, '历史记录 ID'), view: options.view || 'submitted' }; }
    } else if (action === 'candidate') {
        if (subaction === 'preview') { op = 'candidate-preview'; args.candidateId = required(options.candidateId, '候选 ID'); }
        else if (subaction === 'adopt') { op = 'candidate-adopt'; args = { candidateId: required(options.candidateId, '候选 ID'), keys: list(options.keys, '字段 key'), confirmLocked: options.confirmLocked === true }; }
    } else if (action === 'reveal') {
        if (subaction === 'prepare') {
            op = 'reveal-prepare';
            args.kind = required(options.kind, 'reveal 类型');
            if (!['merged', 'image', 'candidate', 'field', 'record'].includes(args.kind)) invalid('reveal 类型无效。');
            if (args.kind === 'merged') {
                if (options.id !== undefined) invalid('合并文字无需指定 --id。');
            } else args.id = required(options.id, '内容 ID');
            if (options.ticket !== undefined || options.toAiContext) invalid('准备票据时不能使用 consume 参数。');
        } else if (subaction === 'consume') {
            if (options.json !== true || options.toAiContext !== true) throw new CliError('interaction_required', '单次展示需要同时明确 --json 和 --to-ai-context。', EXIT.interaction);
            if (options.kind !== undefined || options.id !== undefined) invalid('消费票据时无需重新指定类型或 ID。');
            op = 'reveal-consume'; args.ticket = required(options.ticket, 'reveal 票据');
        }
    } else if (action === 'tasks') {
        const structured = await readInput(options, stdin);
        if (structured && (options.url || options.file)) invalid('URL 或文件路径不能同时通过参数和 JSON 指定。');
        if (subaction === 'create-url') {
            const input = structured?.data;
            if (input && (Object.keys(input).some((key) => !['url', 'force', 'accept_partial'].includes(key)) || input.force !== undefined && typeof input.force !== 'boolean' || input.accept_partial !== undefined && typeof input.accept_partial !== 'boolean')) invalid('工作区 URL 任务 JSON 只接受 url、force、accept_partial。');
            if (input && Boolean(options.force) !== (input.force === true)) invalid('重抓需要 --force 与 JSON 中的 force=true 同时明确指定。');
            if (input && options.acceptPartial && input.accept_partial !== true) invalid('跳过未完成图片时，JSON 中也需设置 accept_partial=true。');
            op = 'task-create-url'; args = { url: required(input?.url ?? options.url, 'Telegraph 链接'), force: options.force === true, acceptPartial: input?.accept_partial === true || options.acceptPartial === true };
        }
        else if (subaction === 'upload') {
            if (options.force) invalid('上传任务不支持 --force。');
            const input = structured?.data;
            if (input && (Object.keys(input).some((key) => !['file', 'accept_partial'].includes(key)) || input.accept_partial !== undefined && typeof input.accept_partial !== 'boolean')) invalid('工作区上传 JSON 只接受 file、accept_partial。');
            if (input && options.acceptPartial && input.accept_partial !== true) invalid('跳过未完成图片时，JSON 中也需设置 accept_partial=true。');
            op = 'task-upload'; args = { file: required(input?.file ?? options.file, '本地压缩包'), acceptPartial: input?.accept_partial === true || options.acceptPartial === true };
        }
    } else if (action === 'ai') {
        if (subaction === 'prepare') { op = 'ai-prepare'; args = { keys: list(options.keys, '字段 key'), acceptPartial: options.acceptPartial === true }; }
        else if (subaction === 'edit') { op = 'ai-edit'; args = await input(options, stdin, ['text']); }
        else if (subaction === 'retain') op = 'ai-retain';
        else if (subaction === 'extract') {
            requireInteractive({ json: options.json, interactive });
            const review = await rpc(id, { op: 'ai-review' });
            const approved = await ttyDecision(`目标：${review.target}\n模型：${review.model_id}\n字段：${review.field_keys.join('、')}\n本次发送文字：\n${review.text}`, { interactive });
            if (!approved) throw new CliError('cancelled', '用户未确认发送。', EXIT.interaction);
            op = 'ai-extract'; args.ticket = review.ticket;
        }
    }
    if (!op) throw new CliError('invalid_command', '未知工作区命令。');
    const reads = new Set(['status', 'review', 'merge', 'provider-list', 'candidate-preview', 'validate', 'document', 'ai-review']);
    const expectedRevision = reads.has(op) ? undefined : intValue(options.revision, '工作区 revision');
    return rpc(id, { op, args, ...(expectedRevision === undefined ? {} : { expectedRevision }) },
        op === 'task-upload' ? { timeoutMs: 11 * 60_000 } : {});
}

export async function runMetadata(action, options, { stdin = process.stdin } = {}) {
    if (!['schema', 'validate', 'history', 'providers'].includes(action)) throw new CliError('invalid_command', '未知元数据命令。');
    const { client, session } = await authenticatedContext(options);
    const get = async (path) => (await client.request('GET', path, { cookie: session.cookie })).data;
    if (action === 'schema') {
        const data = await get('/api/metadata/schema');
        if (data?.schema_version !== 1 || !data.definitions) throw new CliError('invalid_response', '字段定义响应无效。', EXIT.transport);
        return { schema_version: data.schema_version, definitions_version: data.definitions_version,
            fields: Object.entries(data.definitions).map(([key, item]) => ({ key, type: item.type, enabled: item.enabled === true, editable: item.editable === true, extractable: item.extractable || [] })) };
    }
    if (action === 'history') {
        const limit = options.limit === undefined ? 20 : intValue(options.limit, '历史条数');
        const data = await get(`/api/metadata-history?limit=${limit}`);
        if (!Array.isArray(data)) throw new CliError('invalid_response', '历史响应无效。', EXIT.transport);
        return { entries: data.map((item) => ({ task_id: item.task_id, has_submitted: Boolean(item.metadata_document), has_effective: Boolean(item.effective_metadata_document) })) };
    }
    if (action === 'providers') {
        const data = await get('/api/metadata/providers');
        if (!Array.isArray(data?.sources)) throw new CliError('invalid_response', '来源响应无效。', EXIT.transport);
        return { sources: data.sources.map((item) => ({ provider_id: item.provider_id, enabled: item.enabled === true, config_version: item.config_version, visibility: item.visibility })) };
    }
    const value = await input(options, stdin, ['document']);
    const data = (await client.request('POST', '/api/metadata/validate', { cookie: session.cookie, csrf: session.csrf, body: value })).data;
    return { valid: true, document_revision: data.document?.revision, warning_codes: (data.warnings || []).map((item) => ({ key: item.key, code: item.code })) };
}

const JSONL_OPERATIONS = new Set([
    'status', 'image-add', 'image-languages', 'image-edit', 'image-accept-raw', 'image-retry', 'image-remove', 'image-move', 'image-cancel',
    'text-set', 'merge', 'field-set', 'field-clear', 'field-unlock', 'rules-preview', 'provider-list', 'provider-search', 'provider-resolve',
    'history-list', 'history-select', 'candidate-preview', 'candidate-adopt', 'ai-prepare', 'ai-edit', 'ai-retain', 'validate',
    'task-create-url', 'task-upload', 'close',
]);
const MAX_JSONL_LINE = 1024 * 1024;

export async function runWorkspaceJSONL(options, { stdin = process.stdin, stdout = process.stdout, rpc = workspaceRPC } = {}) {
    const id = required(options.workspace, '工作区 ID');
    const emit = async (value) => {
        if (stdout.write(`${JSON.stringify(value)}\n`) === false) await new Promise((resolve) => stdout.once('drain', resolve));
    };
    let pending = Buffer.alloc(0); let dropping = false; let sequence = 0;
    async function processLine(bytes) {
        if (bytes.length > MAX_JSONL_LINE) { await emit({ seq: sequence++, ok: false, code: 'invalid_input', message: 'JSONL 请求超过 1 MiB。' }); return; }
        let request;
        try { request = JSON.parse(bytes.toString('utf8')); }
        catch { await emit({ seq: sequence++, ok: false, code: 'invalid_input', message: 'JSONL 请求格式无效。' }); return; }
        const seq = sequence++;
        try {
            if (!request || typeof request !== 'object' || Array.isArray(request) || !JSONL_OPERATIONS.has(request.op) || request.args !== undefined && (typeof request.args !== 'object' || !request.args || Array.isArray(request.args))) invalid('JSONL 操作无效。');
            const reads = new Set(['status', 'merge', 'provider-list', 'candidate-preview', 'validate']);
            const expectedRevision = reads.has(request.op) ? undefined : intValue(request.expected_revision, '工作区 expected_revision');
            const data = await rpc(id, { op: request.op, args: request.args || {}, ...(expectedRevision === undefined ? {} : { expectedRevision }) },
                request.op === 'task-upload' ? { timeoutMs: 11 * 60_000 } : {});
            await emit({ seq, ok: true, code: 'ok', data });
            if (request.op === 'close') return false;
        } catch (error) {
            const safe = safeError(error); await emit({ seq, ok: false, code: safe.code, message: safe.message });
        }
        return true;
    }
    for await (const chunk of stdin) {
        const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
        let start = 0;
        while (start < bytes.length) {
            const newline = bytes.indexOf(10, start);
            const end = newline < 0 ? bytes.length : newline;
            const piece = bytes.subarray(start, end);
            if (!dropping) {
                if (pending.length + piece.length > MAX_JSONL_LINE) { dropping = true; pending = Buffer.alloc(0); }
                else pending = Buffer.concat([pending, piece]);
            }
            if (newline >= 0) {
                if (dropping) { await emit({ seq: sequence++, ok: false, code: 'invalid_input', message: 'JSONL 请求超过 1 MiB。' }); dropping = false; }
                else if (pending.length && await processLine(pending) === false) return;
                pending = Buffer.alloc(0);
            }
            start = end + 1;
        }
    }
    if (pending.length || dropping) await emit({ seq: sequence, ok: false, code: 'invalid_input', message: 'JSONL 最后一条请求未以换行结束。' });
}
