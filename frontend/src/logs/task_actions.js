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
