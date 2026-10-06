import { buildDuplicateActions } from './form_submission.js';

export function resolveDownloadSubmission(payload, basePayload) {
    const resolved = payload && typeof payload === 'object' ? payload : {};

    if (resolved.duplicate && basePayload?.idempotency_key) {
        // Keyed creation rejects mismatched source/metadata snapshots on the
        // server. Reusing its matching task needs no second confirmation.
        const taskId = resolved.task_id || resolved.task?.id;
        if (typeof taskId !== 'string' || !taskId.trim()) throw new Error('无法读取已有任务，请重试本次提交。');
        return {
            kind: resolved.active ? 'duplicate_active' : 'duplicate_existing',
            feedback: { message: '已复用内容一致的已有任务。', kind: 'info' },
            actions: { logsUrl: resolved.logs_url || '/logs' },
            pendingDuplicate: null,
        };
    }

    if (resolved.duplicate && resolved.active) {
        return {
            kind: 'duplicate_active',
            feedback: { message: '该链接已在下载队列中。', kind: 'info' },
            actions: { logsUrl: resolved.logs_url || '/logs' },
            pendingDuplicate: null,
        };
    }

    if (resolved.duplicate && (resolved.needs_confirmation || resolved.download_url)) {
        return {
            kind: 'duplicate_confirm',
            feedback: { message: '该文件已有下载，是否生成新的CBZ文件？', kind: 'info' },
            actions: { needsDuplicateChoices: true },
            pendingDuplicate: buildDuplicateActions(resolved, basePayload),
        };
    }

    return {
        kind: 'queued',
        feedback: { message: '任务已加入队列。', kind: 'success' },
        actions: { logsUrl: resolved.logs_url || '/logs' },
        pendingDuplicate: null,
    };
}
