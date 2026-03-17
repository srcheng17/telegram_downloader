import { createTasksApi } from '../shared/api/tasks_api.js';
import { buildDuplicateActions } from './form_submission.js';
import { renderSummary, syncSummaryCollapseMode } from './summary_panel.js';
import { createStartupRecoveryBannerController } from './startup_recovery_banner.js';
import { localizeServerMessage } from '../shared/server_messages.js';

export function createHomeModule(win, doc) {
    const state = win.__telegraphHomeState || {
        form: null,
        submitHandler: null,
        summaryTimer: null,
        inflightSummaryController: null,
        startupRecoveryDismissKey: '',
        startupRecoveryDismissHandler: null,
        pendingDuplicate: null,
        summaryResizeHandler: null,
    };
    win.__telegraphHomeState = state;
    const STARTUP_RECOVERY_SESSION_KEY_PREFIX = 'telegraph.startup_recovery.dismissed.';
    const api = createTasksApi((url, options) => win.fetch(url, options));
    const startupRecoveryBanner = createStartupRecoveryBannerController(win, doc, STARTUP_RECOVERY_SESSION_KEY_PREFIX);

    function extractPayloadMessage(payload) {
        if (!payload || typeof payload !== 'object') {
            return '';
        }
        return localizeServerMessage(payload.message, 'home');
    }

    function dismissStartupRecoveryBanner() {
        startupRecoveryBanner.dismiss();
    }

    function updateSummary(summary) {
        startupRecoveryBanner.render(summary);
        renderSummary(doc, summary);
    }

    function showFeedback(message, kind) {
        const feedback = doc.getElementById('download-feedback');
        if (!feedback) {
            return;
        }
        feedback.textContent = message || '';
        feedback.classList.remove('feedback-success', 'feedback-error', 'feedback-info', 'is-visible');
        if (kind) {
            feedback.classList.add(`feedback-${kind}`);
        }
        if (message) {
            feedback.classList.add('is-visible');
        }
    }

    function setActionButton(button, options) {
        if (!button) {
            return false;
        }

        const resolvedOptions = options && typeof options === 'object' ? options : {};
        const isVisible = Boolean(resolvedOptions.visible);
        if (!isVisible) {
            button.classList.add('is-hidden');
            button.onclick = null;
            button.disabled = false;
            return false;
        }

        if (resolvedOptions.label) {
            button.textContent = resolvedOptions.label;
        }

        button.disabled = false;
        button.classList.remove('is-hidden');

        if (typeof resolvedOptions.onClick === 'function') {
            button.onclick = resolvedOptions.onClick;
            return true;
        }

        const targetUrl = resolvedOptions.url ? String(resolvedOptions.url).trim() : '';
        if (!targetUrl) {
            button.classList.add('is-hidden');
            button.onclick = null;
            return false;
        }

        button.onclick = () => {
            win.location.href = targetUrl;
        };
        return true;
    }

    function clearPendingDuplicate() {
        state.pendingDuplicate = null;
    }

    function resetActionButtons() {
        const actions = doc.getElementById('download-actions');
        const logsButton = doc.getElementById('download-action-logs');
        const downloadButton = doc.getElementById('download-action-download');
        const forceButton = doc.getElementById('download-action-force');
        const useExistingButton = doc.getElementById('download-action-use-existing');

        setActionButton(logsButton, { visible: false });
        setActionButton(downloadButton, { visible: false });
        setActionButton(forceButton, { visible: false });
        setActionButton(useExistingButton, { visible: false });

        if (!actions) {
            return;
        }
        actions.classList.add('is-hidden');
        actions.setAttribute('aria-hidden', 'true');
    }

    function showActionButtons(options) {
        const actions = doc.getElementById('download-actions');
        if (!actions) {
            return;
        }
        const resolved = options && typeof options === 'object' ? options : {};
        const logsButton = doc.getElementById('download-action-logs');
        const downloadButton = doc.getElementById('download-action-download');
        const forceButton = doc.getElementById('download-action-force');
        const useExistingButton = doc.getElementById('download-action-use-existing');

        const hasLogs = setActionButton(logsButton, {
            visible: Boolean(resolved.logsUrl),
            url: resolved.logsUrl,
            label: '查看日志',
        });
        const hasDownload = setActionButton(downloadButton, {
            visible: Boolean(resolved.downloadUrl),
            url: resolved.downloadUrl,
            label: resolved.downloadLabel || '立即下载',
        });
        const hasForce = setActionButton(forceButton, {
            visible: typeof resolved.onForce === 'function',
            onClick: resolved.onForce,
            label: '生成新的CBZ',
        });
        const hasUseExisting = setActionButton(useExistingButton, {
            visible: typeof resolved.onUseExisting === 'function',
            onClick: resolved.onUseExisting,
            label: '取消并下载已有文件',
        });

        if (hasLogs || hasDownload || hasForce || hasUseExisting) {
            actions.classList.remove('is-hidden');
            actions.setAttribute('aria-hidden', 'false');
            return;
        }
        actions.classList.add('is-hidden');
        actions.setAttribute('aria-hidden', 'true');
    }

    function setSubmitting(isSubmitting) {
        const button = doc.querySelector('#download-form button[type="submit"]');
        if (!button) {
            return;
        }
        button.disabled = Boolean(isSubmitting);
        button.textContent = isSubmitting ? '提交中...' : '开始下载';
    }

    function readForceValue(form) {
        if (!form) {
            return null;
        }
        const forceControl = form.querySelector('[name="force"]');
        if (!forceControl) {
            return null;
        }

        if (forceControl.matches('input[type="checkbox"]')) {
            return forceControl.checked ? 'true' : 'false';
        }
        if (forceControl.matches('input[type="radio"]')) {
            const checkedRadio = form.querySelector('input[name="force"]:checked');
            return checkedRadio ? String(checkedRadio.value || '').trim() : '';
        }
        return String(forceControl.value || '').trim();
    }

    function readOptionalField(form, fieldName) {
        if (!form || !fieldName) {
            return '';
        }
        const input = form.querySelector(`[name="${fieldName}"]`);
        return input ? String(input.value || '').trim() : '';
    }

    function collectFormPayload(form) {
        const url = readOptionalField(form, 'url');
        const payload = {
            url,
            author: readOptionalField(form, 'author'),
            series_name: readOptionalField(form, 'series_name'),
            comic_name: readOptionalField(form, 'comic_name'),
            summary: readOptionalField(form, 'summary'),
            tags: readOptionalField(form, 'tags'),
            genres: readOptionalField(form, 'genres'),
        };
        const forceValue = readForceValue(form);
        if (forceValue !== null) {
            payload.force = forceValue;
        }
        return payload;
    }

    function buildRequestErrorMessage(payload, statusCode) {
        return extractPayloadMessage(payload) || `请求失败（${statusCode}）`;
    }

    async function postDownloadRequest(formPayload) {
        return api.postForm('/download', formPayload);
    }

    function showExistingDownloadEntry() {
        const duplicateState = state.pendingDuplicate;
        if (!duplicateState) {
            return;
        }

        showFeedback('已取消创建新任务，可直接下载已有文件。', 'info');
        showActionButtons({
            downloadUrl: duplicateState.downloadUrl,
            downloadLabel: '下载已有文件',
            logsUrl: duplicateState.logsUrl,
        });
        clearPendingDuplicate();
    }

    async function submitForceDuplicate() {
        const duplicateState = state.pendingDuplicate;
        if (!duplicateState) {
            return;
        }

        const retryPayload = {
            ...duplicateState.basePayload,
            force: 'true',
        };

        setSubmitting(true);
        resetActionButtons();
        showFeedback('正在创建新的 CBZ 任务...', 'info');

        try {
            const { response, payload } = await postDownloadRequest(retryPayload);
            if (!response.ok || !payload || payload.ok !== true) {
                showFeedback(buildRequestErrorMessage(payload, response.status), 'error');
                showActionButtons({
                    onForce: submitForceDuplicate,
                    onUseExisting: showExistingDownloadEntry,
                });
                return;
            }
            handleDownloadSuccess(payload, duplicateState.basePayload);
        } catch (error) {
            console.error('Failed to resubmit forced download:', error);
            showFeedback('网络异常，请稍后重试。', 'error');
            showActionButtons({
                onForce: submitForceDuplicate,
                onUseExisting: showExistingDownloadEntry,
            });
        } finally {
            setSubmitting(false);
        }
    }

    function handleDownloadSuccess(payload, basePayload) {
        if (payload.duplicate && payload.active) {
            clearPendingDuplicate();
            showFeedback('该链接已在下载队列中。', 'info');
            showActionButtons({ logsUrl: payload.logs_url || '/logs' });
            return;
        }

        if (payload.duplicate && (payload.needs_confirmation || payload.download_url)) {
            state.pendingDuplicate = buildDuplicateActions(payload, basePayload);
            showFeedback('该文件已有下载，是否生成新的CBZ文件？', 'info');
            showActionButtons({
                onForce: submitForceDuplicate,
                onUseExisting: showExistingDownloadEntry,
            });
            return;
        }

        clearPendingDuplicate();
        showFeedback('任务已加入队列。', 'success');
        fetchSummary();
        showActionButtons({ logsUrl: payload.logs_url || '/logs' });
    }

    async function fetchSummary() {
        if (!doc.getElementById('summary-panel')) {
            return;
        }
        if (state.inflightSummaryController) {
            state.inflightSummaryController.abort();
        }

        const controller = new AbortController();
        state.inflightSummaryController = controller;
        try {
            const { response, payload } = await api.getJson('/api/summary', {
                cache: 'no-store',
                signal: controller.signal,
            });
            if (!response.ok || !payload) {
                return;
            }
            updateSummary(payload);
        } catch (error) {
            if (error && error.name !== 'AbortError') {
                console.error('Failed to load summary:', error);
            }
        } finally {
            if (state.inflightSummaryController === controller) {
                state.inflightSummaryController = null;
            }
        }
    }

    async function submitDownload(event) {
        event.preventDefault();

        const form = event.currentTarget;
        const formPayload = collectFormPayload(form);
        const url = (formPayload.url || '').trim();

        resetActionButtons();
        clearPendingDuplicate();

        if (!url) {
            showFeedback('请先输入 Telegraph 链接。', 'error');
            return;
        }

        setSubmitting(true);
        showFeedback('正在提交任务...', 'info');

        try {
            const { response, payload } = await postDownloadRequest(formPayload);
            if (!response.ok || !payload || payload.ok !== true) {
                showFeedback(buildRequestErrorMessage(payload, response.status), 'error');
                return;
            }

            handleDownloadSuccess(payload, formPayload);
        } catch (error) {
            console.error('Failed to submit download:', error);
            showFeedback('网络异常，请稍后重试。', 'error');
            resetActionButtons();
        } finally {
            setSubmitting(false);
        }
    }

    function clearTimers() {
        if (state.summaryTimer) {
            clearInterval(state.summaryTimer);
            state.summaryTimer = null;
        }
    }

    function mount() {
        const form = doc.getElementById('download-form');
        const summaryPanel = doc.getElementById('summary-panel');
        const startupRecoveryDismissBtn = doc.getElementById('startup-recovery-dismiss-home');
        if (!form && !summaryPanel) {
            return;
        }

        if (state.form && state.submitHandler) {
            state.form.removeEventListener('submit', state.submitHandler);
        }
        if (state.startupRecoveryDismissHandler && startupRecoveryDismissBtn) {
            startupRecoveryDismissBtn.removeEventListener('click', state.startupRecoveryDismissHandler);
        }
        clearTimers();
        if (state.summaryResizeHandler) {
            win.removeEventListener('resize', state.summaryResizeHandler);
            state.summaryResizeHandler = null;
        }

        if (form) {
            state.form = form;
            state.submitHandler = submitDownload;
            form.addEventListener('submit', state.submitHandler);
            resetActionButtons();
            clearPendingDuplicate();
            setSubmitting(false);
        } else {
            state.form = null;
            state.submitHandler = null;
        }

        state.startupRecoveryDismissHandler = dismissStartupRecoveryBanner;
        if (startupRecoveryDismissBtn) {
            startupRecoveryDismissBtn.addEventListener('click', state.startupRecoveryDismissHandler);
        }

        if (summaryPanel) {
            syncSummaryCollapseMode(win, doc);
            state.summaryResizeHandler = () => syncSummaryCollapseMode(win, doc);
            win.addEventListener('resize', state.summaryResizeHandler);
            fetchSummary();
            state.summaryTimer = win.setInterval(fetchSummary, 10000);
        } else {
            startupRecoveryBanner.hide();
        }
    }

    function unmount() {
        const startupRecoveryDismissBtn = doc.getElementById('startup-recovery-dismiss-home');
        if (state.form && state.submitHandler) {
            state.form.removeEventListener('submit', state.submitHandler);
        }
        if (startupRecoveryDismissBtn && state.startupRecoveryDismissHandler) {
            startupRecoveryDismissBtn.removeEventListener('click', state.startupRecoveryDismissHandler);
        }
        state.form = null;
        state.submitHandler = null;
        state.startupRecoveryDismissHandler = null;
        state.startupRecoveryDismissKey = '';
        if (state.summaryResizeHandler) {
            win.removeEventListener('resize', state.summaryResizeHandler);
            state.summaryResizeHandler = null;
        }

        if (state.inflightSummaryController) {
            state.inflightSummaryController.abort();
            state.inflightSummaryController = null;
        }

        startupRecoveryBanner.hide();
        resetActionButtons();
        clearPendingDuplicate();
        clearTimers();
    }

    return {
        mount,
        unmount,
    };
}
