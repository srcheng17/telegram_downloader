(function () {
    const state = window.__telegraphLogsState || {
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
    window.__telegraphLogsState = state;
    const STARTUP_RECOVERY_SESSION_KEY_PREFIX = 'telegraph.startup_recovery.dismissed.';
    const SERVER_MESSAGE_TRANSLATIONS = {
        'Stored file is unavailable.': '缓存文件不可用。',
        'Task not found.': '任务不存在。',
        'Task is not completed yet.': '任务尚未完成。',
        'Output file not found for this task.': '任务输出文件不存在。',
        'Cancellation requested.': '已提交取消请求。',
        'Cancellation already requested.': '已提交取消请求。',
    };

    function localizeServerMessage(message) {
        const normalized = String(message || '').trim();
        if (!normalized) {
            return '';
        }
        if (normalized.startsWith('Task already finished with status ')) {
            return '任务已结束，无法取消。';
        }
        return SERVER_MESSAGE_TRANSLATIONS[normalized] || normalized;
    }

    function showFeedback(message, kind) {
        const feedback = document.getElementById('logs-feedback');
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
        return String(status || '').trim().toUpperCase();
    }

    function fallbackStatusLabel(statusCode) {
        return normalizeStatusCode(statusCode).replace(/_/g, ' ') || '未知状态';
    }

    function getStatusMeta(status) {
        const statusCode = normalizeStatusCode(status);
        return state.statusCatalog[statusCode] || null;
    }

    function renderStatusFilterOptions() {
        const statusInput = document.getElementById('status-filter');
        if (!statusInput) {
            return;
        }
        const selectedValue = normalizeStatusCode(state.filters.status || statusInput.value);

        statusInput.innerHTML = '';
        const allOption = document.createElement('option');
        allOption.value = '';
        allOption.textContent = '全部';
        statusInput.appendChild(allOption);

        Object.entries(state.statusCatalog).forEach(([statusCode, metadata]) => {
            const option = document.createElement('option');
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
        if (!rawCatalog || typeof rawCatalog !== 'object') {
            return;
        }
        const nextCatalog = {};
        Object.entries(rawCatalog).forEach(([rawStatus, rawMeta]) => {
            const statusCode = normalizeStatusCode(rawStatus);
            if (!statusCode) {
                return;
            }
            const metadata = rawMeta && typeof rawMeta === 'object' ? rawMeta : {};
            nextCatalog[statusCode] = {
                label:
                    typeof metadata.label === 'string' && metadata.label.trim()
                        ? metadata.label.trim()
                        : fallbackStatusLabel(statusCode),
                can_cancel: Boolean(metadata.can_cancel),
                can_download: Boolean(metadata.can_download),
            };
        });
        if (!Object.keys(nextCatalog).length) {
            return;
        }
        state.statusCatalog = nextCatalog;
        renderStatusFilterOptions();
    }

    function toNonNegativeInteger(value) {
        const parsed = Number(value);
        if (!Number.isFinite(parsed) || parsed < 0) {
            return 0;
        }
        return Math.round(parsed);
    }

    function parseStartupRecovery(summary) {
        if (!summary || typeof summary !== 'object') {
            return null;
        }
        const raw = summary.startup_recovery;
        if (!raw || typeof raw !== 'object') {
            return null;
        }

        const recoveredFailed = toNonNegativeInteger(raw.recovered_failed ?? raw.failed_tasks);
        const recoveredCanceled = toNonNegativeInteger(raw.recovered_canceled ?? raw.canceled_tasks);
        const recoveredTotal = Math.max(
            toNonNegativeInteger(raw.recovered_total ?? raw.total),
            recoveredFailed + recoveredCanceled,
        );
        const happened = raw.happened === true || raw.happened === 'true' || recoveredTotal > 0;
        if (!happened) {
            return null;
        }
        const signature = String(
            raw.event_id ||
                raw.summary_id ||
                raw.happened_at ||
                raw.occurred_at ||
                `${recoveredTotal}-${recoveredFailed}-${recoveredCanceled}`,
        );

        return {
            recoveredFailed,
            recoveredCanceled,
            recoveredTotal,
            signature,
        };
    }

    function getStartupRecoveryDismissKey(signature) {
        return `${STARTUP_RECOVERY_SESSION_KEY_PREFIX}${signature}`;
    }

    function isStartupRecoveryDismissed(dismissKey) {
        if (!dismissKey) {
            return false;
        }
        try {
            return window.sessionStorage.getItem(dismissKey) === '1';
        } catch (error) {
            return false;
        }
    }

    function rememberStartupRecoveryDismissed(dismissKey) {
        if (!dismissKey) {
            return;
        }
        try {
            window.sessionStorage.setItem(dismissKey, '1');
        } catch (error) {
            // Ignore session storage failures (private mode, browser policy, etc.).
        }
    }

    function hideStartupRecoveryBanner() {
        const banner = document.getElementById('startup-recovery-banner-logs');
        if (!banner) {
            return;
        }
        banner.classList.add('is-hidden');
        banner.setAttribute('aria-hidden', 'true');
    }

    function buildStartupRecoveryMessage(recovery) {
        if (recovery.recoveredTotal > 0) {
            return `启动恢复已处理 ${recovery.recoveredTotal} 个任务（失败 ${recovery.recoveredFailed}，取消 ${recovery.recoveredCanceled}）。`;
        }
        return '启动恢复已完成，任务状态已更新。';
    }

    function renderStartupRecoveryBanner(summary) {
        const banner = document.getElementById('startup-recovery-banner-logs');
        const messageNode = document.getElementById('startup-recovery-text-logs');
        if (!banner || !messageNode) {
            return;
        }

        const recovery = parseStartupRecovery(summary);
        if (!recovery) {
            state.startupRecoveryDismissKey = '';
            hideStartupRecoveryBanner();
            return;
        }

        const dismissKey = getStartupRecoveryDismissKey(recovery.signature);
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
        const modal = document.getElementById('error-modal');
        const content = document.getElementById('error-modal-content');
        const closeButton = document.getElementById('error-modal-close');
        if (!modal || !content || !closeButton) {
            return;
        }
        state.modalRestoreFocusEl = triggerElement || document.activeElement;
        content.textContent = errorText || '';
        modal.classList.remove('hidden');
        document.body.classList.add('modal-open');
        closeButton.focus();
    }

    function hideErrorModal() {
        const modal = document.getElementById('error-modal');
        const content = document.getElementById('error-modal-content');
        if (!modal || !content) {
            return;
        }
        modal.classList.add('hidden');
        content.textContent = '';
        document.body.classList.remove('modal-open');

        // Return focus to the "Details" trigger for keyboard users.
        const restoreFocusEl = state.modalRestoreFocusEl;
        state.modalRestoreFocusEl = null;
        if (restoreFocusEl && typeof restoreFocusEl.focus === 'function' && document.body.contains(restoreFocusEl)) {
            restoreFocusEl.focus();
        }
    }

    function createValueContainer() {
        const container = document.createElement('div');
        container.className = 'log-cell-value';
        return container;
    }

    function appendTextValue(container, text) {
        const value = document.createElement('span');
        value.className = 'log-cell-text';
        value.textContent = String(text || '');
        container.appendChild(value);
    }

    function createCell(text) {
        const cell = document.createElement('td');
        const value = createValueContainer();
        appendTextValue(value, text);
        cell.appendChild(value);
        return cell;
    }

    function createLinkCell(url) {
        const cell = document.createElement('td');
        const value = createValueContainer();
        if (!url) {
            appendTextValue(value, '');
            cell.appendChild(value);
            return cell;
        }
        const link = document.createElement('a');
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
        const safeStatus = normalizeStatusCode(status) || 'UNKNOWN';
        const classSuffix = safeStatus.toLowerCase().replace(/[^a-z_]/g, '');
        const statusMeta = getStatusMeta(safeStatus);
        const cell = document.createElement('td');
        const value = createValueContainer();
        const badge = document.createElement('span');
        badge.className = `status-badge status-${classSuffix || 'unknown'}`;
        badge.textContent = statusMeta ? statusMeta.label : fallbackStatusLabel(safeStatus);
        badge.title = safeStatus;
        value.appendChild(badge);
        cell.appendChild(value);
        return cell;
    }

    function createErrorCell(errorText) {
        const cell = document.createElement('td');
        const value = createValueContainer();
        const message = String(errorText || '').trim();
        if (!message) {
            appendTextValue(value, '-');
            cell.appendChild(value);
            return cell;
        }
        const preview = document.createElement('div');
        preview.className = 'error-preview';
        preview.textContent = message;
        value.appendChild(preview);

        if (message.length > 120) {
            const detailsButton = document.createElement('button');
            detailsButton.type = 'button';
            detailsButton.className = 'btn-details';
            detailsButton.textContent = '详情';
            detailsButton.addEventListener('click', () => showErrorModal(message, detailsButton));
            value.appendChild(detailsButton);
        }
        cell.appendChild(value);
        return cell;
    }

    async function readJsonPayload(response) {
        if (!response) {
            return null;
        }
        try {
            return await response.json();
        } catch (error) {
            return null;
        }
    }

    function extractPayloadMessage(payload) {
        if (!payload || typeof payload !== 'object') {
            return '';
        }
        const message = typeof payload.message === 'string' ? payload.message.trim() : '';
        return localizeServerMessage(message);
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
            const response = await fetch(`/api/tasks/${encodeURIComponent(taskId)}/cancel`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                cache: 'no-store',
            });
            const payload = await readJsonPayload(response);
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
        const existing = document.getElementById('download-transport-frame');
        if (existing) {
            return existing;
        }
        const iframe = document.createElement('iframe');
        iframe.id = 'download-transport-frame';
        iframe.title = '下载中转';
        iframe.setAttribute('aria-hidden', 'true');
        iframe.style.display = 'none';
        document.body.appendChild(iframe);
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
            const response = await fetch(downloadUrl, {
                method: 'GET',
                headers: { Accept: 'application/json' },
                cache: 'no-store',
            });
            const payload = await readJsonPayload(response);
            return extractPayloadMessage(payload) || fallbackMessage;
        } catch (error) {
            console.error('Failed to load download error details:', error);
            return fallbackMessage;
        }
    }

    async function precheckDownload(downloadUrl) {
        const response = await fetch(downloadUrl, {
            method: 'HEAD',
            cache: 'no-store',
        });

        if (response.ok || response.status === 405 || response.status === 501) {
            return { ok: true };
        }

        const message = await fetchDownloadErrorMessage(downloadUrl, response.status);
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
            const downloadUrl = `/api/tasks/${encodeURIComponent(taskId)}/download`;
            const precheckResult = await precheckDownload(downloadUrl);
            if (!precheckResult.ok) {
                showFeedback(precheckResult.message || '下载失败。', 'error');
                return;
            }

            // Let the browser stream the response directly instead of buffering a Blob in memory.
            triggerNativeDownload(downloadUrl);
            showFeedback('下载已开始。', 'success');
        } catch (error) {
            console.error('Error downloading task file:', error);
            showFeedback('下载启动失败，请重试。', 'error');
        } finally {
            setTimeout(() => {
                state.downloadInProgressTaskIds.delete(taskId);
                if (button && document.body.contains(button)) {
                    button.disabled = false;
                    button.textContent = '下载';
                }
            }, 1200);
        }
    }

    function createActionCell(log) {
        const cell = document.createElement('td');
        cell.className = 'log-action-cell';
        const value = createValueContainer();
        const status = normalizeStatusCode(log.status);
        const statusMeta = getStatusMeta(status);
        const canCancel = Boolean(statusMeta && statusMeta.can_cancel && log.id);
        const canDownload = Boolean(statusMeta && statusMeta.can_download && log.id);

        if (canCancel) {
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'btn-cancel';
            button.textContent = '取消';
            button.onclick = () => requestCancel(log.id, button);
            value.appendChild(button);
            cell.appendChild(value);
            return cell;
        }

        if (canDownload) {
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'btn-download';
            button.textContent = '下载';
            button.onclick = () => requestDownload(log.id, button);
            value.appendChild(button);
            cell.appendChild(value);
            return cell;
        }

        appendTextValue(value, '-');
        cell.appendChild(value);
        return cell;
    }

    function renderLogs(logs) {
        const logBody = document.getElementById('log-body');
        if (!logBody) {
            return;
        }

        logBody.innerHTML = '';
        if (!logs.length) {
            const row = document.createElement('tr');
            const cell = document.createElement('td');
            cell.colSpan = 7;
            cell.style.textAlign = 'center';
            cell.textContent = '当前筛选条件下暂无日志。';
            row.appendChild(cell);
            logBody.appendChild(row);
            return;
        }

        const fragment = document.createDocumentFragment();
        logs.forEach((log) => {
            const row = document.createElement('tr');
            const progress = log.total_images > 0 ? `${log.progress} / ${log.total_images}` : '暂无';
            row.appendChild(createCell(log.id || ''));
            row.appendChild(createLinkCell(log.url || ''));
            row.appendChild(createStatusCell(log.status));
            row.appendChild(createCell(progress));
            row.appendChild(createCell(new Date((log.start_time || 0) * 1000).toLocaleString()));
            row.appendChild(createErrorCell(log.error || ''));
            row.appendChild(createActionCell(log));
            fragment.appendChild(row);
        });
        logBody.appendChild(fragment);
    }

    function renderLogsLoadFailure(message) {
        const logBody = document.getElementById('log-body');
        if (!logBody) {
            return;
        }
        logBody.innerHTML = '';
        const row = document.createElement('tr');
        const cell = document.createElement('td');
        cell.colSpan = 7;
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
        const active = document.getElementById('summary-active-count');
        const finished = document.getElementById('summary-finished-count');
        const successRate = document.getElementById('summary-success-rate');
        const failures = document.getElementById('summary-failure-count');

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
        const summaryCollapsible = document.getElementById('summary-collapsible-logs');
        if (!summaryCollapsible || typeof window.matchMedia !== 'function') {
            return;
        }
        const isMobile = window.matchMedia('(max-width: 768px)').matches;
        summaryCollapsible.open = !isMobile;
    }

    function updatePaginationUI() {
        const pageInfo = document.getElementById('page-info');
        const prevBtn = document.getElementById('prev-page');
        const nextBtn = document.getElementById('next-page');
        if (!pageInfo || !prevBtn || !nextBtn) {
            return;
        }
        pageInfo.textContent = `第 ${state.currentPage} / ${state.totalPages} 页`;
        prevBtn.disabled = state.currentPage <= 1;
        nextBtn.disabled = state.currentPage >= state.totalPages;
    }

    function syncFormWithFilters() {
        const statusInput = document.getElementById('status-filter');
        const queryInput = document.getElementById('query-filter');
        const normalizedStatus = normalizeStatusCode(state.filters.status);
        if (statusInput && statusInput.value !== normalizedStatus) {
            statusInput.value = normalizedStatus;
        }
        if (queryInput && queryInput.value !== state.filters.q) {
            queryInput.value = state.filters.q;
        }
    }

    function readFiltersFromForm() {
        const statusInput = document.getElementById('status-filter');
        const queryInput = document.getElementById('query-filter');
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

    function getFailureBackoffDelay() {
        if (state.consecutiveFailures <= 0) {
            return 0;
        }
        const exponentialDelay = state.activePollMs * 2 ** (state.consecutiveFailures - 1);
        return Math.min(state.maxBackoffMs, exponentialDelay);
    }

    function resolvePollDelay(baseDelayMs) {
        const visibilityFloorMs = document.visibilityState === 'visible' ? 0 : state.hiddenPollMs;
        return Math.max(baseDelayMs, visibilityFloorMs, getFailureBackoffDelay());
    }

    function scheduleNextFetch(delayMs) {
        if (state.pollTimer) {
            clearTimeout(state.pollTimer);
        }
        if (!document.getElementById('logs-page')) {
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
        let nextDelayMs = resolvePollDelay(state.idlePollMs);
        let shouldSchedule = true;

        try {
            const response = await fetch(buildLogsUrl(page), {
                signal: controller.signal,
                cache: 'no-store',
            });
            if (!response.ok) {
                const httpError = new Error(`HTTP ${response.status}`);
                httpError.statusCode = response.status;
                throw httpError;
            }
            const data = await response.json();
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
            nextDelayMs = resolvePollDelay(data.has_active_tasks ? state.activePollMs : state.idlePollMs);
        } catch (error) {
            if (error && error.name === 'AbortError') {
                shouldSchedule = false;
                return;
            }
            state.consecutiveFailures += 1;
            nextDelayMs = resolvePollDelay(state.activePollMs);
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
        const root = document.getElementById('logs-page');
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
            window.removeEventListener('resize', state.summaryResizeHandler);
            state.summaryResizeHandler = null;
        }
        renderStatusFilterOptions();
        syncFormWithFilters();

        const filterForm = document.getElementById('logs-filter-form');
        const clearBtn = document.getElementById('clear-filters');
        const prevBtn = document.getElementById('prev-page');
        const nextBtn = document.getElementById('next-page');
        const startupRecoveryDismissBtn = document.getElementById('startup-recovery-dismiss-logs');
        const modalCloseBtn = document.getElementById('error-modal-close');
        const modalBackdrop = document.getElementById('error-modal-backdrop');

        state.formHandler = onFilterSubmit;
        state.clearHandler = onClearFilters;
        state.prevHandler = onPrevPage;
        state.nextHandler = onNextPage;
        state.startupRecoveryDismissHandler = dismissStartupRecoveryBanner;
        state.modalCloseHandler = hideErrorModal;
        state.modalBackdropHandler = hideErrorModal;
        state.visibilityHandler = function () {
            if (!document.getElementById('logs-page')) {
                return;
            }
            if (document.visibilityState === 'visible') {
                if (state.pollTimer) {
                    clearTimeout(state.pollTimer);
                    state.pollTimer = null;
                }
                fetchLogs(state.currentPage);
                return;
            }
            scheduleNextFetch(resolvePollDelay(state.idlePollMs));
        };
        state.keyDownHandler = function (event) {
            const modal = document.getElementById('error-modal');
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
            const activeElement = document.activeElement;

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
        document.addEventListener('visibilitychange', state.visibilityHandler);
        document.addEventListener('keydown', state.keyDownHandler);
        syncSummaryCollapseMode();
        state.summaryResizeHandler = syncSummaryCollapseMode;
        window.addEventListener('resize', state.summaryResizeHandler);

        fetchLogs(1);
    }

    function unmount() {
        const filterForm = document.getElementById('logs-filter-form');
        const clearBtn = document.getElementById('clear-filters');
        const prevBtn = document.getElementById('prev-page');
        const nextBtn = document.getElementById('next-page');
        const startupRecoveryDismissBtn = document.getElementById('startup-recovery-dismiss-logs');
        const modalCloseBtn = document.getElementById('error-modal-close');
        const modalBackdrop = document.getElementById('error-modal-backdrop');

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
            document.removeEventListener('visibilitychange', state.visibilityHandler);
        }
        if (state.keyDownHandler) {
            document.removeEventListener('keydown', state.keyDownHandler);
        }
        if (state.summaryResizeHandler) {
            window.removeEventListener('resize', state.summaryResizeHandler);
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

    window.TelegraphDownloaderLogs = {
        mount,
        unmount,
    };
})();
