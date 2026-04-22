function normalizeActionName(action) {
    return String(action || '')
        .trim()
        .toLowerCase();
}

function getBackendAvailableActions(task) {
    if (!task || typeof task !== 'object') {
        return null;
    }
    if (Array.isArray(task.available_actions)) {
        return task.available_actions.map(normalizeActionName).filter(Boolean);
    }
    if (Array.isArray(task.availableActions)) {
        return task.availableActions.map(normalizeActionName).filter(Boolean);
    }
    return null;
}

function hasBackendAction(task, action) {
    const backendActions = getBackendAvailableActions(task);
    if (backendActions) {
        return backendActions.includes(normalizeActionName(action));
    }
    return null;
}

function hasLegacyAction(task, action) {
    const statusCode = normalizeActionName(task && task.status);
    const taskType = normalizeActionName(task && task.task_type);
    const hasURL = String(task && task.url ? task.url : '').trim() !== '';
    const hasSourceArchive =
        String(task && task.source_archive_path ? task.source_archive_path : task && task.sourceArchivePath ? task.sourceArchivePath : '').trim() !== '';
    switch (normalizeActionName(action)) {
        case 'cancel':
            return ['created', 'ready', 'queued', 'uploading', 'running', 'in_progress', 'canceling'].includes(statusCode);
        case 'retry':
            if (!task || !task.retryable || !['failed', 'canceled'].includes(statusCode)) {
                return false;
            }
            return taskType === 'upload' ? hasSourceArchive : hasURL;
        case 'download':
        case 'copy_to_komga':
            return ['success', 'succeeded'].includes(statusCode);
        default:
            return false;
    }
}

function canUseTaskAction(task, action) {
    const backend = hasBackendAction(task, action);
    if (backend !== null) {
        return backend;
    }
    return hasLegacyAction(task, action);
}

export async function runSuccessTaskAction({ api, taskId, mode, browserDownload }) {
    if (String(mode || '').trim() === 'komga_copy') {
        return api.postJson(`/api/tasks/${encodeURIComponent(taskId)}/copy-to-komga`, {});
    }
    if (typeof browserDownload !== 'function') {
        throw new Error('browser download action is required');
    }
    return browserDownload(taskId);
}

export async function requestRetryTask(api, taskId) {
    return api.postJson(`/api/tasks/${encodeURIComponent(taskId)}/retry`, {});
}

export function canCancelTaskAction(task) {
    return canUseTaskAction(task, 'cancel');
}

export function canRetryTaskAction(task) {
    return canUseTaskAction(task, 'retry');
}

export function canDownloadTaskAction(task) {
    return canUseTaskAction(task, 'download');
}

export function canCopyToKomgaTaskAction(task) {
    return canUseTaskAction(task, 'copy_to_komga');
}
