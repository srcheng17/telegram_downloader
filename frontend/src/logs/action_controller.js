import { normalizeHeadResult } from './download_preflight.js';
import { requestRetryTask, runSuccessTaskAction } from './task_actions.js';
import { localizeServerMessage } from '../shared/server_messages.js';

function extractPayloadMessage(payload) {
    if (!payload || typeof payload !== 'object') {
        return '';
    }
    const message = typeof payload.message === 'string' ? payload.message.trim() : '';
    return localizeServerMessage(message, 'logs');
}

function getDownloadIframe(doc) {
    const existing = doc.getElementById('download-transport-frame');
    if (existing) {
        return existing;
    }
    const iframe = doc.createElement('iframe');
    iframe.id = 'download-transport-frame';
    iframe.title = '下载中转';
    iframe.setAttribute('aria-hidden', 'true');
    iframe.style.display = 'none';
    doc.body.appendChild(iframe);
    return iframe;
}

function triggerNativeDownload(doc, downloadUrl) {
    const iframe = getDownloadIframe(doc);
    const separator = downloadUrl.includes('?') ? '&' : '?';
    iframe.src = `${downloadUrl}${separator}_dlts=${Date.now()}`;
}

export function createLogsActionController({ api, logsApi, win, doc, state, showFeedback, fetchLogs }) {
    async function fetchDownloadErrorMessage(downloadUrl, statusCode) {
        const fallbackMessage = `下载失败（${statusCode}）。`;
        try {
            const { payload } = await api.getJson(downloadUrl, {
                headers: { Accept: 'application/json' },
                cache: 'no-store',
            });
            return extractPayloadMessage(payload) || fallbackMessage;
        } catch (error) {
            console.error('Failed to load download error details:', error);
            return fallbackMessage;
        }
    }

    async function precheckDownload(downloadUrl) {
        const response = await logsApi.head(downloadUrl);
        const normalized = await normalizeHeadResult(response);
        if (normalized.ok) {
            return { ok: true };
        }
        const message = await fetchDownloadErrorMessage(downloadUrl, normalized.status);
        return { ok: false, message };
    }

    async function getDownloadActionMode() {
        const cachedMode =
            win.__telegraphSettingsState && typeof win.__telegraphSettingsState.downloadActionMode === 'string'
                ? String(win.__telegraphSettingsState.downloadActionMode).trim()
                : '';
        if (cachedMode) {
            return cachedMode;
        }
        try {
            const { response, payload } = await logsApi.getSettingsMode();
            if (response.ok && payload && typeof payload.download_action_mode === 'string') {
                const resolvedMode = String(payload.download_action_mode).trim() || 'browser';
                win.__telegraphSettingsState = {
                    ...(win.__telegraphSettingsState || {}),
                    downloadActionMode: resolvedMode,
                };
                return resolvedMode;
            }
        } catch (error) {
            console.error('Failed to load settings mode for logs action:', error);
        }
        return 'browser';
    }

    async function requestCancel(taskId, button) {
        if (!taskId) {
            return;
        }
        if (button) {
            button.disabled = true;
            button.textContent = '取消中...';
        }
        try {
            const { response, payload } = await api.postJson(`/api/tasks/${encodeURIComponent(taskId)}/cancel`, {}, {
                cache: 'no-store',
            });
            const payloadMessage = extractPayloadMessage(payload);
            if (response.ok && payload && payload.ok === true) {
                showFeedback(payloadMessage || '已提交取消请求。', 'success');
                return;
            }
            if (response.status === 409) {
                showFeedback(payloadMessage || '任务已结束，无法取消。', 'error');
                return;
            }
            showFeedback(payloadMessage || `取消任务失败（${response.status}）。`, 'error');
        } catch (error) {
            console.error('Error requesting cancellation:', error);
            showFeedback('取消任务失败。', 'error');
        } finally {
            fetchLogs(state.currentPage);
        }
    }

    async function requestDownload(taskId, button) {
        if (!taskId || state.downloadInProgressTaskIds.has(taskId)) {
            return;
        }
        state.downloadInProgressTaskIds.add(taskId);
        if (button) {
            button.disabled = true;
            button.textContent = '准备中...';
        }

        try {
            const mode = await getDownloadActionMode();
            const result = await runSuccessTaskAction({
                api,
                taskId,
                mode,
                browserDownload: async (resolvedTaskId) => {
                    const downloadUrl = `/api/tasks/${encodeURIComponent(resolvedTaskId)}/download`;
                    const precheckResult = await precheckDownload(downloadUrl);
                    if (!precheckResult.ok) {
                        throw new Error(precheckResult.message || '下载失败。');
                    }
                    triggerNativeDownload(doc, downloadUrl);
                    return { response: { ok: true }, payload: null };
                },
            });
            if (mode === 'komga_copy') {
                const targetPath = result && result.payload ? String(result.payload.target_path || '').trim() : '';
                showFeedback(targetPath ? `已复制到 ${targetPath}` : '已复制到 Komga。', 'success');
                return;
            }
            showFeedback('下载已开始。', 'success');
        } catch (error) {
            console.error('Error handling success task action:', error);
            showFeedback(error && error.message ? error.message : '下载启动失败，请重试。', 'error');
        } finally {
            setTimeout(() => {
                state.downloadInProgressTaskIds.delete(taskId);
                if (button && doc.body.contains(button)) {
                    button.disabled = false;
                    button.textContent = '下载';
                }
            }, 1200);
        }
    }

    async function requestRetry(taskId, button) {
        if (!taskId || state.retryInProgressTaskIds.has(taskId)) {
            return;
        }
        state.retryInProgressTaskIds.add(taskId);
        if (button) {
            button.disabled = true;
            button.textContent = '重试中...';
        }
        try {
            const { response, payload } = await requestRetryTask(api, taskId);
            const payloadMessage = extractPayloadMessage(payload);
            if (response.ok && payload && payload.ok === true) {
                showFeedback(payloadMessage || '已重新加入队列。', 'success');
                return;
            }
            showFeedback(payloadMessage || `重试失败（${response.status}）。`, 'error');
        } catch (error) {
            console.error('Error retrying upload task:', error);
            showFeedback('重试失败，请稍后再试。', 'error');
        } finally {
            state.retryInProgressTaskIds.delete(taskId);
            fetchLogs(state.currentPage);
        }
    }

    return {
        getDownloadActionMode,
        requestCancel,
        requestDownload,
        requestRetry,
    };
}

