import { createTasksApi } from '../shared/api/tasks_api.js';
import { normalizeHeadResult } from './download_preflight.js';
import { hideErrorModal as hideLogsErrorModal, showErrorModal as showLogsErrorModal } from './error_modal.js';
import { applyStatusCatalog as normalizeStatusCatalog } from './status_filters.js';
import { buildStatusBadgeModel } from './table_render.js';
import { mapTaskToLogViewModel } from './view_model.js';
import { requestRetryTask, runSuccessTaskAction } from './task_actions.js';
import {
    fallbackStatusLabel as fallbackSharedStatusLabel,
    getStatusMeta as getSharedStatusMeta,
    normalizeStatusCode as normalizeSharedStatusCode,
} from '../shared/models/status_catalog.js';
import { resolvePollDelay } from '../shared/polling.js';
import {
    buildStartupRecoveryMessage,
    getStartupRecoveryDismissKey,
    parseStartupRecovery,
} from '../shared/startup_recovery.js';
import { localizeServerMessage } from '../shared/server_messages.js';

export function createLogsModule(win, doc) {
    const state = win.__telegraphLogsState || {
        mountedRoot: null,
        currentPage: 1,
        totalPages: 1,
        perPage: 25,
        activePollMs: 2000,
        idlePollMs: 8000,
        hiddenPollMs: 30000,
        maxBackoffMs: 60000,
        consecutiveFailures: 0,
        pollTimer: null,
        inflightController: null,
        filters: { status: '', q: '' },
        statusCatalog: {},
        downloadInProgressTaskIds: new Set(),
        retryInProgressTaskIds: new Set(),
        startupRecoveryDismissKey: '',
        modalRestoreFocusEl: null,
        formHandler: null,
        clearHandler: null,
        prevHandler: null,
        nextHandler: null,
        startupRecoveryDismissHandler: null,
        modalCloseHandler: null,
        modalBackdropHandler: null,
        visibilityHandler: null,
        keyDownHandler: null,
        summaryResizeHandler: null,
    };
    win.__telegraphLogsState = state;
    const STARTUP_RECOVERY_SESSION_KEY_PREFIX = 'telegraph.startup_recovery.dismissed.';
    const api = createTasksApi((url, options) => win.fetch(url, options));
    function showFeedback(message, kind) {
        const feedback = doc.getElementById('logs-feedback');
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

    function normalizeStatusCode(status) {
        return normalizeSharedStatusCode(status);
    }

    function fallbackStatusLabel(statusCode) {
        return fallbackSharedStatusLabel(statusCode);
    }

    function getStatusMeta(status) {
        return getSharedStatusMeta(state.statusCatalog, status);
    }

    function renderStatusFilterOptions() {
        const statusInput = doc.getElementById('status-filter');
        if (!statusInput) {
            return;
        }
        const selectedValue = normalizeStatusCode(state.filters.status || statusInput.value);

        statusInput.innerHTML = '';
        const allOption = doc.createElement('option');
        allOption.value = '';
        allOption.textContent = '全部';
        statusInput.appendChild(allOption);

        Object.entries(state.statusCatalog).forEach(([statusCode, metadata]) => {
            const option = doc.createElement('option');
            option.value = statusCode;
            option.textContent =
                metadata && typeof metadata.label === 'string' && metadata.label.trim()
                    ? metadata.label.trim()
                    : fallbackStatusLabel(statusCode);
            statusInput.appendChild(option);
        });

        if (selectedValue && state.statusCatalog[selectedValue]) {
            statusInput.value = selectedValue;
            return;
        }
        statusInput.value = '';
    }

    function applyStatusCatalog(rawCatalog) {
        const nextCatalog = normalizeStatusCatalog(rawCatalog);
        if (!Object.keys(nextCatalog).length) {
            return;
        }
        state.statusCatalog = nextCatalog;
        renderStatusFilterOptions();
    }

    function isStartupRecoveryDismissed(dismissKey) {
        if (!dismissKey) {
            return false;
        }
        try {
            return win.sessionStorage.getItem(dismissKey) === '1';
        } catch (error) {
            return false;
        }
    }

    function rememberStartupRecoveryDismissed(dismissKey) {
        if (!dismissKey) {
            return;
        }
        try {
            win.sessionStorage.setItem(dismissKey, '1');
        } catch (error) {
            // Ignore session storage failures (private mode, browser policy, etc.).
        }
    }

    function hideStartupRecoveryBanner() {
        const banner = doc.getElementById('startup-recovery-banner-logs');
        if (!banner) {
            return;
        }
        banner.classList.add('is-hidden');
        banner.setAttribute('aria-hidden', 'true');
    }

    function renderStartupRecoveryBanner(summary) {
        const banner = doc.getElementById('startup-recovery-banner-logs');
        const messageNode = doc.getElementById('startup-recovery-text-logs');
        if (!banner || !messageNode) {
            return;
        }

        const recovery = parseStartupRecovery(summary);
        if (!recovery) {
            state.startupRecoveryDismissKey = '';
            hideStartupRecoveryBanner();
            return;
        }

        const dismissKey = getStartupRecoveryDismissKey(STARTUP_RECOVERY_SESSION_KEY_PREFIX, recovery.signature);
        state.startupRecoveryDismissKey = dismissKey;
        if (isStartupRecoveryDismissed(dismissKey)) {
            hideStartupRecoveryBanner();
            return;
        }

        messageNode.textContent = buildStartupRecoveryMessage(recovery);
        banner.classList.remove('is-hidden');
        banner.setAttribute('aria-hidden', 'false');
    }

    function dismissStartupRecoveryBanner() {
        rememberStartupRecoveryDismissed(state.startupRecoveryDismissKey);
        hideStartupRecoveryBanner();
    }

    function getFocusableModalElements(modal) {
        if (!modal) {
            return [];
        }
        const focusableSelector =
            'a[href], area[href], input:not([disabled]):not([type="hidden"]), ' +
            'select:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex="-1"])';
        return Array.from(modal.querySelectorAll(focusableSelector)).filter((element) => element.offsetParent !== null);
    }

    function showErrorModal(errorText, triggerElement) {
        showLogsErrorModal(doc, state, errorText, triggerElement);
    }

    function hideErrorModal() {
        hideLogsErrorModal(doc, state);
    }

    function createValueContainer() {
        const container = doc.createElement('div');
        container.className = 'log-cell-value';
        return container;
    }

    function appendTextValue(container, text) {
        const value = doc.createElement('span');
        value.className = 'log-cell-text';
        value.textContent = String(text || '');
        container.appendChild(value);
    }

    function createCell(text) {
        const cell = doc.createElement('td');
        const value = createValueContainer();
        appendTextValue(value, text);
        cell.appendChild(value);
        return cell;
    }

    function createLinkCell(url) {
        const cell = doc.createElement('td');
        const value = createValueContainer();
        if (!url) {
            appendTextValue(value, '');
            cell.appendChild(value);
            return cell;
        }
        const link = doc.createElement('a');
        link.className = 'log-cell-link';
        link.href = url;
        link.target = '_blank';
        link.rel = 'noopener noreferrer';
        link.textContent = (url || '').split('/').pop().split('?')[0] || url;
        value.appendChild(link);
        cell.appendChild(value);
        return cell;
    }

    function createStatusCell(status) {
        const model = buildStatusBadgeModel(status, state.statusCatalog);
        const cell = doc.createElement('td');
        const value = createValueContainer();
        const badge = doc.createElement('span');
        badge.className = model.className;
        badge.textContent = model.label;
        badge.title = model.statusCode;
        value.appendChild(badge);
        cell.appendChild(value);
        return cell;
    }

    function createTaskTypeCell(log) {
        const view = mapTaskToLogViewModel(log);
        const cell = doc.createElement('td');
        const value = createValueContainer();
        appendTextValue(value, view.taskTypeLabel);
        cell.appendChild(value);
        return cell;
    }

    function createErrorCell(errorText) {
        const cell = doc.createElement('td');
        const value = createValueContainer();
        const message = String(errorText || '').trim();
        if (!message) {
            appendTextValue(value, '-');
            cell.appendChild(value);
            return cell;
        }
        const preview = doc.createElement('div');
        preview.className = 'error-preview';
        preview.textContent = message;
        value.appendChild(preview);

        if (message.length > 120) {
            const detailsButton = doc.createElement('button');
            detailsButton.type = 'button';
            detailsButton.className = 'btn-details';
            detailsButton.textContent = '详情';
            detailsButton.addEventListener('click', () => showErrorModal(message, detailsButton));
            value.appendChild(detailsButton);
        }
        cell.appendChild(value);
        return cell;
    }

    function extractPayloadMessage(payload) {
        if (!payload || typeof payload !== 'object') {
            return '';
        }
        const message = typeof payload.message === 'string' ? payload.message.trim() : '';
        return localizeServerMessage(message, 'logs');
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

    function getDownloadIframe() {
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

    function triggerNativeDownload(downloadUrl) {
        const iframe = getDownloadIframe();
        const separator = downloadUrl.includes('?') ? '&' : '?';
        iframe.src = `${downloadUrl}${separator}_dlts=${Date.now()}`;
    }

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
        const response = await api.head(downloadUrl, {
            cache: 'no-store',
        });
        const normalized = await normalizeHeadResult(response);
        if (normalized.ok) {
            return { ok: true };
        }

        const message = await fetchDownloadErrorMessage(downloadUrl, normalized.status);
        return { ok: false, message };
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
                    triggerNativeDownload(downloadUrl);
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

    async function getDownloadActionMode() {
        const cachedMode =
            win.__telegraphSettingsState && typeof win.__telegraphSettingsState.downloadActionMode === 'string'
                ? String(win.__telegraphSettingsState.downloadActionMode).trim()
                : '';
        if (cachedMode) {
            return cachedMode;
        }
        try {
            const { response, payload } = await api.getJson('/v2/settings', { cache: 'no-store' });
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

    function createActionCell(log) {
        const view = mapTaskToLogViewModel(log);
        const cell = doc.createElement('td');
        cell.className = 'log-action-cell';
        const value = createValueContainer();
        const status = view.status;
        const statusMeta = getStatusMeta(status);
        const canCancel = Boolean(statusMeta && statusMeta.can_cancel && view.id);
        const canDownload = Boolean(statusMeta && statusMeta.can_download && view.id);
        const canRetry = view.canRetry;

        if (canCancel) {
            const button = doc.createElement('button');
            button.type = 'button';
            button.className = 'btn-cancel';
            button.textContent = '取消';
            button.onclick = () => requestCancel(view.id, button);
            value.appendChild(button);
            cell.appendChild(value);
            return cell;
        }

        if (canRetry) {
            const button = doc.createElement('button');
            button.type = 'button';
            button.className = 'btn-secondary';
            button.textContent = '重试';
            button.onclick = () => requestRetry(view.id, button);
            value.appendChild(button);
            cell.appendChild(value);
            return cell;
        }

        if (canDownload) {
            const button = doc.createElement('button');
            button.type = 'button';
            button.className = 'btn-download';
            button.textContent = '下载';
            button.onclick = () => requestDownload(view.id, button);
            value.appendChild(button);
            cell.appendChild(value);
            return cell;
        }

        appendTextValue(value, '-');
        cell.appendChild(value);
        return cell;
    }

    function renderLogs(logs) {
        const logBody = doc.getElementById('log-body');
        if (!logBody) {
            return;
        }

        logBody.innerHTML = '';
        if (!logs.length) {
            const row = doc.createElement('tr');
            const cell = doc.createElement('td');
            cell.colSpan = 8;
            cell.style.textAlign = 'center';
            cell.textContent = '当前筛选条件下暂无日志。';
            row.appendChild(cell);
            logBody.appendChild(row);
            return;
        }

        const fragment = doc.createDocumentFragment();
        logs.forEach((log) => {
            const view = mapTaskToLogViewModel(log);
            const row = doc.createElement('tr');
            row.appendChild(createCell(view.id));
            row.appendChild(createTaskTypeCell(log));
            row.appendChild(createLinkCell(view.url));
            row.appendChild(createStatusCell(view.status));
            row.appendChild(createCell(view.progressText));
            row.appendChild(createCell(view.startTimeLabel));
            row.appendChild(createErrorCell(view.errorText));
            row.appendChild(createActionCell(log));
            fragment.appendChild(row);
        });
        logBody.appendChild(fragment);
    }

    function renderLogsLoadFailure(message) {
        const logBody = doc.getElementById('log-body');
        if (!logBody) {
            return;
        }
        logBody.innerHTML = '';
        const row = doc.createElement('tr');
        const cell = doc.createElement('td');
        cell.colSpan = 8;
        cell.style.textAlign = 'center';
        cell.style.color = 'red';
        cell.textContent = message;
        row.appendChild(cell);
        logBody.appendChild(row);
    }

    function updateSummary(summary) {
        renderStartupRecoveryBanner(summary);
        if (!summary) {
            return;
        }
        const active = doc.getElementById('summary-active-count');
        const finished = doc.getElementById('summary-finished-count');
        const successRate = doc.getElementById('summary-success-rate');
        const failures = doc.getElementById('summary-failure-count');

        if (active) {
            active.textContent = String(summary.active_tasks || 0);
        }
        if (finished) {
            finished.textContent = String(summary.finished_tasks || 0);
        }
        if (successRate) {
            successRate.textContent = summary.success_rate === null ? '-' : `${summary.success_rate}%`;
        }
        if (failures) {
            failures.textContent = String((summary.failed_tasks || 0) + (summary.canceled_tasks || 0));
        }
    }

    function syncSummaryCollapseMode() {
        const summaryCollapsible = doc.getElementById('summary-collapsible-logs');
        if (!summaryCollapsible || typeof win.matchMedia !== 'function') {
            return;
        }
        const isMobile = win.matchMedia('(max-width: 768px)').matches;
        summaryCollapsible.open = !isMobile;
    }

    function updatePaginationUI() {
        const pageInfo = doc.getElementById('page-info');
        const prevBtn = doc.getElementById('prev-page');
        const nextBtn = doc.getElementById('next-page');
        if (!pageInfo || !prevBtn || !nextBtn) {
            return;
        }
        pageInfo.textContent = `第 ${state.currentPage} / ${state.totalPages} 页`;
        prevBtn.disabled = state.currentPage <= 1;
        nextBtn.disabled = state.currentPage >= state.totalPages;
    }

    function syncFormWithFilters() {
        const statusInput = doc.getElementById('status-filter');
        const queryInput = doc.getElementById('query-filter');
        const normalizedStatus = normalizeStatusCode(state.filters.status);
        if (statusInput && statusInput.value !== normalizedStatus) {
            statusInput.value = normalizedStatus;
        }
        if (queryInput && queryInput.value !== state.filters.q) {
            queryInput.value = state.filters.q;
        }
    }

    function readFiltersFromForm() {
        const statusInput = doc.getElementById('status-filter');
        const queryInput = doc.getElementById('query-filter');
        state.filters.status = statusInput ? normalizeStatusCode(statusInput.value) : '';
        state.filters.q = queryInput ? String(queryInput.value || '').trim() : '';
    }

    function buildLogsUrl(page) {
        const params = new URLSearchParams();
        params.set('page', String(page));
        params.set('per_page', String(state.perPage));
        if (state.filters.status) {
            params.set('status', state.filters.status);
        }
        if (state.filters.q) {
            params.set('q', state.filters.q);
        }
        return `/api/logs?${params.toString()}`;
    }

    function resolveNextPollDelay(active) {
        return resolvePollDelay({
            hidden: doc.visibilityState !== 'visible',
            failures: state.consecutiveFailures,
            active,
            activePollMs: state.activePollMs,
            idlePollMs: state.idlePollMs,
            hiddenPollMs: state.hiddenPollMs,
            maxBackoffMs: state.maxBackoffMs,
        });
    }

    function scheduleNextFetch(delayMs) {
        if (state.pollTimer) {
            clearTimeout(state.pollTimer);
        }
        if (!doc.getElementById('logs-page')) {
            return;
        }
        state.pollTimer = setTimeout(() => fetchLogs(state.currentPage), Math.max(0, delayMs));
    }

    async function fetchLogs(page = 1) {
        if (state.inflightController) {
            state.inflightController.abort();
        }
        const controller = new AbortController();
        state.inflightController = controller;
        let nextDelayMs = resolveNextPollDelay(false);
        let shouldSchedule = true;

        try {
            const { response, payload: data } = await api.getJson(buildLogsUrl(page), {
                signal: controller.signal,
                cache: 'no-store',
            });
            if (!response.ok) {
                const httpError = new Error(`HTTP ${response.status}`);
                httpError.statusCode = response.status;
                throw httpError;
            }
            applyStatusCatalog(data.status_catalog);
            renderLogs(data.logs || []);
            updateSummary(data.summary || null);

            state.currentPage = Number(data.page) || 1;
            state.totalPages = Math.max(
                1,
                Number(data.total_pages) || Math.ceil((Number(data.total) || 0) / (Number(data.per_page) || state.perPage)),
            );
            updatePaginationUI();

            const filters = data.filters || {};
            state.filters.status = normalizeStatusCode(filters.status);
            state.filters.q = String(filters.q || '');
            syncFormWithFilters();

            state.consecutiveFailures = 0;
            nextDelayMs = resolveNextPollDelay(Boolean(data.has_active_tasks));
        } catch (error) {
            if (error && error.name === 'AbortError') {
                shouldSchedule = false;
                return;
            }
            state.consecutiveFailures += 1;
            nextDelayMs = resolveNextPollDelay(true);
            console.error('Error fetching logs:', error);
            const statusCode = Number(error && error.statusCode);
            const hasStatusCode = Number.isFinite(statusCode) && statusCode > 0;
            const failureDetail = hasStatusCode
                ? `HTTP ${statusCode}，稍后自动重试，可手动刷新。`
                : '网络异常，稍后自动重试，可手动刷新。';
            renderLogsLoadFailure(`日志刷新失败：${failureDetail}`);
            showFeedback(`日志刷新失败：${failureDetail}`, 'error');
        } finally {
            if (state.inflightController === controller) {
                state.inflightController = null;
            }
            if (shouldSchedule) {
                scheduleNextFetch(nextDelayMs);
            }
        }
    }

    function onFilterSubmit(event) {
        event.preventDefault();
        readFiltersFromForm();
        state.currentPage = 1;
        showFeedback('已应用筛选条件。', 'info');
        fetchLogs(1);
    }

    function onClearFilters() {
        state.filters = { status: '', q: '' };
        syncFormWithFilters();
        state.currentPage = 1;
        showFeedback('筛选条件已重置。', 'info');
        fetchLogs(1);
    }

    function onPrevPage() {
        if (state.currentPage > 1) {
            fetchLogs(state.currentPage - 1);
        }
    }

    function onNextPage() {
        if (state.currentPage < state.totalPages) {
            fetchLogs(state.currentPage + 1);
        }
    }

    function mount() {
        const root = doc.getElementById('logs-page');
        if (!root) {
            return;
        }

        if (state.mountedRoot === root) {
            return;
        }
        if (state.mountedRoot) {
            unmount();
        }

        state.mountedRoot = root;
        state.currentPage = 1;
        state.totalPages = 1;
        state.filters = { status: '', q: '' };
        state.statusCatalog = {};
        state.consecutiveFailures = 0;
        state.startupRecoveryDismissKey = '';
        state.modalRestoreFocusEl = null;
        if (state.summaryResizeHandler) {
            win.removeEventListener('resize', state.summaryResizeHandler);
            state.summaryResizeHandler = null;
        }
        renderStatusFilterOptions();
        syncFormWithFilters();

        const filterForm = doc.getElementById('logs-filter-form');
        const clearBtn = doc.getElementById('clear-filters');
        const prevBtn = doc.getElementById('prev-page');
        const nextBtn = doc.getElementById('next-page');
        const startupRecoveryDismissBtn = doc.getElementById('startup-recovery-dismiss-logs');
        const modalCloseBtn = doc.getElementById('error-modal-close');
        const modalBackdrop = doc.getElementById('error-modal-backdrop');

        state.formHandler = onFilterSubmit;
        state.clearHandler = onClearFilters;
        state.prevHandler = onPrevPage;
        state.nextHandler = onNextPage;
        state.startupRecoveryDismissHandler = dismissStartupRecoveryBanner;
        state.modalCloseHandler = hideErrorModal;
        state.modalBackdropHandler = hideErrorModal;
        state.visibilityHandler = function () {
            if (!doc.getElementById('logs-page')) {
                return;
            }
            if (doc.visibilityState === 'visible') {
                if (state.pollTimer) {
                    clearTimeout(state.pollTimer);
                    state.pollTimer = null;
                }
                fetchLogs(state.currentPage);
                return;
            }
            scheduleNextFetch(resolveNextPollDelay(false));
        };
        state.keyDownHandler = function (event) {
            const modal = doc.getElementById('error-modal');
            if (!modal || modal.classList.contains('hidden')) {
                return;
            }

            if (event.key === 'Escape') {
                event.preventDefault();
                hideErrorModal();
                return;
            }
            if (event.key !== 'Tab') {
                return;
            }

            const focusableElements = getFocusableModalElements(modal);
            if (!focusableElements.length) {
                event.preventDefault();
                return;
            }

            const firstFocusable = focusableElements[0];
            const lastFocusable = focusableElements[focusableElements.length - 1];
            const activeElement = doc.activeElement;

            if (!modal.contains(activeElement)) {
                event.preventDefault();
                firstFocusable.focus();
                return;
            }

            if (event.shiftKey && activeElement === firstFocusable) {
                event.preventDefault();
                lastFocusable.focus();
                return;
            }
            if (!event.shiftKey && activeElement === lastFocusable) {
                event.preventDefault();
                firstFocusable.focus();
            }
        };

        if (filterForm) {
            filterForm.addEventListener('submit', state.formHandler);
        }
        if (clearBtn) {
            clearBtn.addEventListener('click', state.clearHandler);
        }
        if (prevBtn) {
            prevBtn.addEventListener('click', state.prevHandler);
        }
        if (nextBtn) {
            nextBtn.addEventListener('click', state.nextHandler);
        }
        if (startupRecoveryDismissBtn) {
            startupRecoveryDismissBtn.addEventListener('click', state.startupRecoveryDismissHandler);
        }
        if (modalCloseBtn) {
            modalCloseBtn.addEventListener('click', state.modalCloseHandler);
        }
        if (modalBackdrop) {
            modalBackdrop.addEventListener('click', state.modalBackdropHandler);
        }
        doc.addEventListener('visibilitychange', state.visibilityHandler);
        doc.addEventListener('keydown', state.keyDownHandler);
        syncSummaryCollapseMode();
        state.summaryResizeHandler = syncSummaryCollapseMode;
        win.addEventListener('resize', state.summaryResizeHandler);

        fetchLogs(1);
    }

    function unmount() {
        const filterForm = doc.getElementById('logs-filter-form');
        const clearBtn = doc.getElementById('clear-filters');
        const prevBtn = doc.getElementById('prev-page');
        const nextBtn = doc.getElementById('next-page');
        const startupRecoveryDismissBtn = doc.getElementById('startup-recovery-dismiss-logs');
        const modalCloseBtn = doc.getElementById('error-modal-close');
        const modalBackdrop = doc.getElementById('error-modal-backdrop');

        if (filterForm && state.formHandler) {
            filterForm.removeEventListener('submit', state.formHandler);
        }
        if (clearBtn && state.clearHandler) {
            clearBtn.removeEventListener('click', state.clearHandler);
        }
        if (prevBtn && state.prevHandler) {
            prevBtn.removeEventListener('click', state.prevHandler);
        }
        if (nextBtn && state.nextHandler) {
            nextBtn.removeEventListener('click', state.nextHandler);
        }
        if (startupRecoveryDismissBtn && state.startupRecoveryDismissHandler) {
            startupRecoveryDismissBtn.removeEventListener('click', state.startupRecoveryDismissHandler);
        }
        if (modalCloseBtn && state.modalCloseHandler) {
            modalCloseBtn.removeEventListener('click', state.modalCloseHandler);
        }
        if (modalBackdrop && state.modalBackdropHandler) {
            modalBackdrop.removeEventListener('click', state.modalBackdropHandler);
        }
        if (state.visibilityHandler) {
            doc.removeEventListener('visibilitychange', state.visibilityHandler);
        }
        if (state.keyDownHandler) {
            doc.removeEventListener('keydown', state.keyDownHandler);
        }
        if (state.summaryResizeHandler) {
            win.removeEventListener('resize', state.summaryResizeHandler);
        }

        state.formHandler = null;
        state.clearHandler = null;
        state.prevHandler = null;
        state.nextHandler = null;
        state.startupRecoveryDismissHandler = null;
        state.modalCloseHandler = null;
        state.modalBackdropHandler = null;
        state.visibilityHandler = null;
        state.keyDownHandler = null;
        state.summaryResizeHandler = null;
        state.statusCatalog = {};
        state.consecutiveFailures = 0;
        state.startupRecoveryDismissKey = '';
        state.modalRestoreFocusEl = null;

        if (state.pollTimer) {
            clearTimeout(state.pollTimer);
            state.pollTimer = null;
        }
        if (state.inflightController) {
            state.inflightController.abort();
            state.inflightController = null;
        }

        hideErrorModal();
        hideStartupRecoveryBanner();
        state.mountedRoot = null;
    }

    return {
        mount,
        unmount,
    };
}
