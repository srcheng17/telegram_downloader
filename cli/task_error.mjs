import { constants, writeSync } from 'node:fs';
import { open } from 'node:fs/promises';
import { authenticatedContext } from './session_context.mjs';
import { safeTask } from './task_projection.mjs';
import { requireInteractive } from './tty.mjs';
import { CliError, EXIT } from './errors.mjs';

const MAX_ERROR_BYTES = 8192;

async function reviewInTerminal(value) {
    let handle;
    try {
        handle = await open('/dev/tty', constants.O_WRONLY | constants.O_NOCTTY);
        writeSync(handle.fd, `${JSON.stringify(value, null, 2)}\n`);
    } catch {
        throw new CliError('interaction_required', '无法打开独立用户终端。', EXIT.interaction);
    } finally { await handle?.close(); }
}

export async function runTaskErrorReview(options, {
    reviewer = reviewInTerminal,
    interactive = options.interactive ?? Boolean(process.stdin.isTTY && process.stderr.isTTY),
} = {}) {
    requireInteractive({ json: options.json, interactive });
    const id = options.id;
    if (typeof id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$/.test(id)) {
        throw new CliError('invalid_input', '任务 ID 无效。');
    }
    const { client, session } = await authenticatedContext(options);
    const response = await client.request('GET', `/api/tasks/${id}`, { cookie: session.cookie });
    const raw = response.data?.task || response.data;
    const task = safeTask(raw);
    if (task.id !== id || raw.error !== undefined && typeof raw.error !== 'string') {
        throw new CliError('invalid_response', '任务详情响应无效。', EXIT.transport);
    }
    const bytes = Buffer.from(raw.error || '', 'utf8');
    const truncated = bytes.length > MAX_ERROR_BYTES;
    await reviewer({ task_id: id, status: task.status,
        error_text: bytes.subarray(0, MAX_ERROR_BYTES).toString('utf8'), truncated });
    return { task_id: id, status: task.status, has_error: bytes.length > 0,
        details_reviewed: true, truncated };
}
