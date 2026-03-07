(function () {
    'use strict';

    const state = window.__telegraphHomeState || {
        form: null,
        submitHandler: null,
        summaryTimer: null,
        inflightSummaryController: null,
        startupRecoveryDismissKey: '',
        startupRecoveryDismissHandler: null,
        pendingDuplicate: null,
        summaryResizeHandler: null,
    };
    window.__telegraphHomeState = state;
    const STARTUP_RECOVERY_SESSION_KEY_PREFIX = 'telegraph.startup_recovery.dismissed.';

    const SERVER_MESSAGE_TRANSLATIONS = {
        'Please provide a Telegraph URL.': '请先输入 Telegraph 链接。',
        'Only telegra.ph or graph.org URLs are supported.': '仅支持 telegra.ph 或 graph.org 链接。',
    };

    function localizeServerMessage(message) {
        const normalized = String(message || '').trim();
        if (!normalized) {
            return '';
        }
        return SERVER_MESSAGE_TRANSLATIONS[normalized] || normalized;
    }

    function extractPayloadMessage(payload) {
        if (!payload || typeof payload !== 'object') {
            return '';
        }
        return localizeServerMessage(payload.message);
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
        const banner = document.getElementById('startup-recovery-banner-home');
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
        const banner = document.getElementById('startup-recovery-banner-home');
        const messageNode = document.getElementById('startup-recovery-text-home');
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

    function updateSummary(summary) {
        renderStartupRecoveryBanner(summary);
        if (!summary) {
            return;
        }
        const totalNode = document.getElementById('summary-total');
        const activeNode = document.getElementById('summary-active');
        const successNode = document.getElementById('summary-success');
        const failedNode = document.getElementById('summary-failed');

        if (totalNode) {
            totalNode.textContent = String(summary.total_tasks || 0);
        }
        if (activeNode) {
            activeNode.textContent = String(summary.active_tasks || 0);
        }
        if (successNode) {
            successNode.textContent = String(summary.success_tasks || 0);
        }
        if (failedNode) {
            failedNode.textContent = String((summary.failed_tasks || 0) + (summary.canceled_tasks || 0));
        }
    }

    function syncSummaryCollapseMode() {
        const summaryCollapsible = document.getElementById('summary-collapsible-home');
        if (!summaryCollapsible || typeof window.matchMedia !== 'function') {
            return;
        }
        const isMobile = window.matchMedia('(max-width: 768px)').matches;
        summaryCollapsible.open = !isMobile;
    }

    function showFeedback(message, kind) {
        const feedback = document.getElementById('download-feedback');
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
            window.location.href = targetUrl;
        };
        return true;
    }

    function clearPendingDuplicate() {
        state.pendingDuplicate = null;
    }

    function resetActionButtons() {
        const actions = document.getElementById('download-actions');
        const logsButton = document.getElementById('download-action-logs');
        const downloadButton = document.getElementById('download-action-download');
        const forceButton = document.getElementById('download-action-force');
        const useExistingButton = document.getElementById('download-action-use-existing');

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
        const actions = document.getElementById('download-actions');
        if (!actions) {
            return;
        }
        const resolved = options && typeof options === 'object' ? options : {};
        const logsButton = document.getElementById('download-action-logs');
        const downloadButton = document.getElementById('download-action-download');
        const forceButton = document.getElementById('download-action-force');
        const useExistingButton = document.getElementById('download-action-use-existing');

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
        const button = document.querySelector('#download-form button[type="submit"]');
        if (!button) {
            return;
        }
        button.disabled = Boolean(isSubmitting);
        button.textContent = isSubmitting ? '提交中...' : '开始下载';
    }

    function toFormUrlEncoded(data) {
        return Object.keys(data)
            .map((key) => `${encodeURIComponent(key)}=${encodeURIComponent(data[key])}`)
            .join('&');
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
        const response = await fetch('/download', {
            method: 'POST',
            headers: {
                Accept: 'application/json',
                'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8',
            },
            body: toFormUrlEncoded(formPayload),
        });
        const payload = await response.json().catch(() => null);
        return { response, payload };
    }

    function basePayloadForDuplicateRetry(formPayload) {
        const payload = { ...formPayload };
        delete payload.force;
        return payload;
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
            state.pendingDuplicate = {
                basePayload: basePayloadForDuplicateRetry(basePayload),
                downloadUrl: payload.download_url || '',
                logsUrl: payload.logs_url || '/logs',
            };
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
        if (!document.getElementById('summary-panel')) {
            return;
        }
        if (state.inflightSummaryController) {
            state.inflightSummaryController.abort();
        }

        const controller = new AbortController();
        state.inflightSummaryController = controller;
        try {
            const response = await fetch('/api/summary', {
                method: 'GET',
                cache: 'no-store',
                signal: controller.signal,
            });
            if (!response.ok) {
                return;
            }
            const payload = await response.json();
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
        const form = document.getElementById('download-form');
        const summaryPanel = document.getElementById('summary-panel');
        const startupRecoveryDismissBtn = document.getElementById('startup-recovery-dismiss-home');
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
            window.removeEventListener('resize', state.summaryResizeHandler);
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
            syncSummaryCollapseMode();
            state.summaryResizeHandler = syncSummaryCollapseMode;
            window.addEventListener('resize', state.summaryResizeHandler);
            fetchSummary();
            state.summaryTimer = window.setInterval(fetchSummary, 10000);
        } else {
            hideStartupRecoveryBanner();
        }
    }

    function unmount() {
        const startupRecoveryDismissBtn = document.getElementById('startup-recovery-dismiss-home');
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
            window.removeEventListener('resize', state.summaryResizeHandler);
            state.summaryResizeHandler = null;
        }

        if (state.inflightSummaryController) {
            state.inflightSummaryController.abort();
            state.inflightSummaryController = null;
        }

        hideStartupRecoveryBanner();
        resetActionButtons();
        clearPendingDuplicate();
        clearTimers();
    }

    window.TelegraphDownloaderHome = {
        mount,
        unmount,
    };
})();
