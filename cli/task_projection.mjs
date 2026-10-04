import { CliError, EXIT } from './errors.mjs';

const actions = new Set(['cancel', 'retry', 'download', 'copy_to_komga']);
const statuses = new Set(['CREATED', 'READY', 'RUNNING', 'CANCELING', 'SUCCEEDED', 'FAILED', 'CANCELED']);
const summaryKeys = [
    'total_tasks', 'pending_tasks', 'in_progress_tasks', 'cancel_requested_tasks',
    'canceled_tasks', 'success_tasks', 'failed_tasks', 'active_tasks', 'finished_tasks',
];

export function safeTask(raw) {
    if (!raw || typeof raw !== 'object' || typeof raw.id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$/.test(raw.id) || !statuses.has(raw.status)) {
        throw new CliError('invalid_response', '任务响应格式无效。', EXIT.transport);
    }
    const progress = raw.progress && typeof raw.progress === 'object' ? raw.progress : {};
    return {
        id: raw.id,
        task_type: ['url', 'upload', 'telegram'].includes(raw.task_type) ? raw.task_type : 'unknown',
        status: raw.status,
        has_error: typeof raw.error === 'string' && raw.error.length > 0,
        available_actions: Array.isArray(raw.available_actions) ? raw.available_actions.filter((action) => actions.has(action)) : [],
        progress: {
            phase: typeof progress.phase === 'string' && /^[a-z_]{1,40}$/.test(progress.phase) ? progress.phase : 'unknown',
            current: Number.isSafeInteger(progress.current) && progress.current >= 0 ? progress.current : 0,
            total: Number.isSafeInteger(progress.total) && progress.total >= 0 ? progress.total : 0,
            unit: ['none', 'bytes', 'images'].includes(progress.unit) ? progress.unit : 'none',
        },
    };
}

export function safeSummary(raw) {
    if (!raw || typeof raw !== 'object') throw new CliError('invalid_response', '概览响应格式无效。', EXIT.transport);
    const result = {};
    for (const key of summaryKeys) result[key] = Number.isSafeInteger(raw[key]) && raw[key] >= 0 ? raw[key] : 0;
    result.success_rate = typeof raw.success_rate === 'number' && Number.isFinite(raw.success_rate) ? raw.success_rate : null;
    const recovery = raw.startup_recovery && typeof raw.startup_recovery === 'object' ? raw.startup_recovery : {};
    result.startup_recovery = {
        happened: recovery.happened === true,
        recovered_total: Number.isSafeInteger(recovery.recovered_total) && recovery.recovered_total >= 0 ? recovery.recovered_total : 0,
        recovered_failed: Number.isSafeInteger(recovery.recovered_failed) && recovery.recovered_failed >= 0 ? recovery.recovered_failed : 0,
        recovered_canceled: Number.isSafeInteger(recovery.recovered_canceled) && recovery.recovered_canceled >= 0 ? recovery.recovered_canceled : 0,
    };
    return result;
}

export function terminal(status) {
    return status === 'SUCCEEDED' || status === 'FAILED' || status === 'CANCELED';
}
