import { formatFieldValue } from '../shared/metadata/schema.js';
import { createTasksApi } from '../shared/api/tasks_api.js';
import { createHomeApi } from './api.js';
import { resetHomeActionButtons, showHomeActionButtons } from './action_buttons.js';
import { applyInputMode, getSelectedMode } from './input_mode.js';
import { renderMetadataHistory } from './metadata_history.js';
import { readOptionalField, readForceValue } from './state.js';
import { createWorkspaceShell } from '../ui_shell/index.js';
import { candidateFromHistory, readHistoryDocument } from '../shared/metadata/candidates.js';
import { resolveDownloadSubmission } from './submit_flow.js';
import { submitArchive } from './upload_submission.js';
import { bindFieldHintToggles } from './field_hints.js';
import { renderSummary, syncSummaryCollapseMode } from './summary_panel.js';
import { createStartupRecoveryBannerController } from './startup_recovery_banner.js';
import { localizeServerMessage } from '../shared/server_messages.js';
import { resolvePollDelay } from '../shared/polling.js';
import { createMetadataSearchModule } from '../metadata-search/index.js';
import { mountOCR } from '../ocr/index.js';
import { createTabs } from '../shared/tabs.js';

export async function retryMetadataWorkspace(state, showFeedback) {
    if (state.ocr?.isDirty() || state.shell?.hasUnsavedChanges()) {
        showFeedback('已保留当前元数据和识别文字，请先处理未提交修改再重新读取字段。', 'info');
        return false;
    }
    // Stop all callbacks targeting the old draft before shell.retry disposes it,
    // including when the replacement schema request subsequently fails.
    state.ocr?.dispose(); state.ocr = null;
    state.metadataSearch?.unmount(); state.metadataSearch = null;
    return Boolean(await state.shell?.retry());
}

export function createHomeModule(win, doc) {
    const state = win.__telegraphHomeState || {
        mountedRoot: null,
        pageController: null,
        submitting: false,
        summaryFailures: 0,
        visibilityHandler: null,
        form: null,
        submitHandler: null,
        summaryTimer: null,
        inflightSummaryController: null,
        startupRecoveryDismissKey: '',
        startupRecoveryDismissHandler: null,
        pendingDuplicate: null,
        summaryResizeHandler: null,
        inputModeHandler: null,
        inputModeNodes: [],
        hintCleanup: null,
        shell: null,
        ocr: null,
        metadataSearch: null,
        retryNode: null,
        retryHandler: null,
        historySequence: 0,
        historyController: null,
    };
    win.__telegraphHomeState = state;
    const STARTUP_RECOVERY_SESSION_KEY_PREFIX = 'telegraph.startup_recovery.dismissed.';
    const api = createTasksApi((url, options) => win.fetch(url, options), { win });
    const homeApi = createHomeApi(api);
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

    function clearPendingDuplicate() {
        state.pendingDuplicate = null;
    }

    function isCurrentPage(controller) {
        return state.pageController === controller && controller && !controller.signal.aborted;
    }

    function setSubmitting(isSubmitting, label = '提交中...') {
        state.submitting = Boolean(isSubmitting);
        const button = doc.querySelector('#download-form button[type="submit"]');
        if (!button) {
            return;
        }
        button.disabled = Boolean(isSubmitting);
        button.textContent = isSubmitting ? label : '开始下载';
    }

    function buildRequestErrorMessage(payload, statusCode) {
        return extractPayloadMessage(payload) || `请求失败（${statusCode}）`;
    }

    async function postDownloadRequest(formPayload, controller) {
        return homeApi.postDownload(formPayload, { signal: controller.signal });
    }

    function showExistingDownloadEntry() {
        const duplicateState = state.pendingDuplicate;
        if (!duplicateState) {
            return;
        }

        showFeedback('已取消创建新任务，可直接下载已有文件。', 'info');
        showHomeActionButtons(doc, {
            downloadUrl: duplicateState.downloadUrl,
            downloadLabel: '下载已有文件',
            logsUrl: duplicateState.logsUrl,
        }, win);
        clearPendingDuplicate();
    }

    async function submitForceDuplicate() {
        const controller = state.pageController;
        if (state.submitting || !isCurrentPage(controller)) return;
        const duplicateState = state.pendingDuplicate;
        if (!duplicateState) {
            return;
        }

        const retryPayload = {
            ...duplicateState.basePayload,
            force: 'true',
        };

        setSubmitting(true);
        resetHomeActionButtons(doc);
        showFeedback('正在创建新的 CBZ 任务...', 'info');

        try {
            const { response, payload } = await postDownloadRequest(retryPayload, controller);
            if (!isCurrentPage(controller)) return;
            if (!response.ok || !payload || payload.ok !== true) {
                showFeedback(buildRequestErrorMessage(payload, response.status), 'error');
                showHomeActionButtons(doc, {
                    onForce: submitForceDuplicate,
                    onUseExisting: showExistingDownloadEntry,
                }, win);
                return;
            }
            handleDownloadSuccess(payload, duplicateState.basePayload);
        } catch (error) {
            if (!isCurrentPage(controller)) return;
            console.error('Failed to resubmit forced download:', error);
            showFeedback('网络异常，请稍后重试。', 'error');
            showHomeActionButtons(doc, {
                onForce: submitForceDuplicate,
                onUseExisting: showExistingDownloadEntry,
            }, win);
        } finally {
            if (isCurrentPage(controller)) setSubmitting(false);
        }
    }

    function handleDownloadSuccess(payload, basePayload) {
        const resolution = resolveDownloadSubmission(payload, basePayload);
        state.pendingDuplicate = resolution.pendingDuplicate;
        showFeedback(resolution.feedback.message, resolution.feedback.kind);

        if (resolution.kind === 'duplicate_confirm') {
            showHomeActionButtons(doc, {
                onForce: submitForceDuplicate,
                onUseExisting: showExistingDownloadEntry,
            }, win);
            return;
        }

        if (resolution.kind === 'queued') {
            markSubmitted(JSON.parse(basePayload.metadata_document));
            fetchSummary();
        }

        showHomeActionButtons(doc, { logsUrl: resolution.actions.logsUrl || '/logs' }, win);
    }

    function scheduleSummary(active) {
        clearTimers();
        if (!state.mountedRoot || doc.visibilityState === 'hidden') return;
        const delay = resolvePollDelay({ active, failures: state.summaryFailures, activePollMs: 10000, idlePollMs: 10000 });
        state.summaryTimer = win.setTimeout(fetchSummary, delay);
    }

    async function fetchSummary() {
        if (!state.mountedRoot || doc.visibilityState === 'hidden' || !doc.getElementById('summary-panel') || state.inflightSummaryController) return;
        clearTimers();
        const controller = new AbortController();
        state.inflightSummaryController = controller;
        let active = false;
        try {
            const { response, payload } = await homeApi.getSummary({ signal: controller.signal });
            if (controller.signal.aborted || state.inflightSummaryController !== controller) return;
            if (!response.ok || !payload) throw new Error(`HTTP ${response.status}`);
            updateSummary(payload);
            active = Number(payload.active_tasks) > 0;
            state.summaryFailures = 0;
        } catch (error) {
            if (controller.signal.aborted || state.inflightSummaryController !== controller) return;
            state.summaryFailures += 1;
            console.error('Failed to load summary:', error);
        } finally {
            if (state.inflightSummaryController === controller) {
                state.inflightSummaryController = null;
                scheduleSummary(active);
            }
        }
    }

    function showHistoryCandidate(entry, view = 'submitted') {
        try {
            const source = readHistoryDocument(entry, view);
            const preview = doc.querySelector('[data-history-values]');
            if (preview) {
                preview.replaceChildren();
                for (const [key, field] of Object.entries(source?.fields || {})) {
                    const definition = source.definition_snapshot[key] || { label: key, type: 'string' };
                    const name = doc.createElement('dt');
                    name.textContent = definition.label;
                    const value = doc.createElement('dd');
                    value.textContent = field.state === 'cleared' ? '已明确清空' : formatFieldValue(definition, field.value);
                    preview.append(name, value);
                }
            }
            const draft = state.shell?.getDraft();
            if (!draft) throw new Error('请等待元数据字段加载后再选择历史记录。');
            state.shell.showCandidate(candidateFromHistory({ entry, view, draft, requestId: String(++state.historySequence) }));
            showFeedback(view === 'effective' ? '正在核对最终归档信息。' : '正在核对历史提交信息。请选择要采用的字段。', 'info');
        } catch (error) { showFeedback(error.message, 'error'); }
    }

    function markSubmitted(document) {
        if (document) state.shell?.markClean(document.revision);
        fetchMetadataHistory();
    }

    async function fetchMetadataHistory() {
        const controller = state.pageController;
        state.historyController?.abort();
        const historyController = new AbortController();
        state.historyController = historyController;
        try {
            const { response, payload } = await homeApi.getMetadataHistory({ signal: historyController.signal });
            if (!isCurrentPage(controller) || state.historyController !== historyController || historyController.signal.aborted) return;
            if (!response.ok || !Array.isArray(payload)) {
                return;
            }
            renderMetadataHistory(doc, payload, (entry) => {
                if (!isCurrentPage(controller) || !state.form) {
                    return;
                }
                const preview = doc.querySelector('[data-history-preview]');
                if (preview) {
                    preview.replaceChildren();
                    const values = doc.createElement('dl');
                    values.dataset.historyValues = '';
                    for (const [view, label] of [['submitted', '提交信息'], ['effective', '最终归档信息']]) {
                        const button = doc.createElement('button');
                        button.type = 'button';
                        button.className = 'btn-secondary';
                        button.dataset.historyView = view;
                        button.textContent = label;
                        button.disabled = view === 'effective' && !entry.effective_metadata_document;
                        button.onclick = () => showHistoryCandidate(entry, view);
                        preview.appendChild(button);
                    }
                    preview.appendChild(values);
                }
                showHistoryCandidate(entry);
            });
        } catch (error) {
            if (!isCurrentPage(controller)) return;
            if (!historyController.signal.aborted) console.error('Failed to load metadata history:', error);
        } finally { if (state.historyController === historyController) state.historyController = null; }
    }

    async function submitUpload(metadataDocument) {
        const controller = state.pageController;
        const archiveInput = doc.getElementById('archive_file');
        const file = archiveInput && archiveInput.files && archiveInput.files[0] ? archiveInput.files[0] : null;
        if (!file) {
            showFeedback('请先选择压缩包文件。', 'error');
            return;
        }

        const metadataPayload = { metadata_document: metadataDocument };
        let initPayload = null;
        setSubmitting(true, '上传中...');
        showFeedback('正在创建上传任务...', 'info');

        try {
            const result = await submitArchive({
                api,
                file,
                metadata: metadataPayload,
                signal: controller.signal,
                onInit(payload) {
                    initPayload = payload;
                    if (!isCurrentPage(controller)) return;
                    showFeedback('上传任务已创建，正在上传压缩包...', 'info');
                    showHomeActionButtons(doc, { logsUrl: payload.logs_url || '/logs' }, win);
                },
                onProgress(snapshot) {
                    if (!isCurrentPage(controller)) return;
                    showFeedback(`正在上传 ${snapshot.fileName}（${snapshot.loadedBytes} / ${snapshot.totalBytes}）`, 'info');
                },
                createXHR: typeof win.XMLHttpRequest === 'function' ? () => new win.XMLHttpRequest() : undefined,
            });
            if (!isCurrentPage(controller)) return;
            markSubmitted(metadataDocument);
            showFeedback('任务已加入队列。', 'success');
            fetchSummary();
            showHomeActionButtons(doc, { logsUrl: (initPayload && initPayload.logs_url) || (result.uploadPayload && result.uploadPayload.logs_url) || '/logs' }, win);
        } catch (error) {
            if (initPayload && initPayload.task_id) {
                api.postJson(`/api/tasks/${encodeURIComponent(initPayload.task_id)}/cancel`, {}).then(({ response }) => {
                    if (!response.ok && response.status !== 409) throw new Error(`HTTP ${response.status}`);
                }).catch((cancelError) => console.error('Failed to cancel interrupted upload:', cancelError));
            }
            if (!isCurrentPage(controller)) return;
            console.error('Failed to submit upload task:', error);
            showFeedback(error && error.message ? error.message : '上传失败，请稍后重试。', 'error');
            if (initPayload) {
                showHomeActionButtons(doc, { logsUrl: initPayload.logs_url || '/logs' }, win);
            } else {
                resetHomeActionButtons(doc, win);
            }
        } finally {
            if (isCurrentPage(controller)) setSubmitting(false);
        }
    }

    async function submitForm(event) {
        event.preventDefault();
        const controller = state.pageController;
        if (state.submitting || !isCurrentPage(controller)) return;

        const form = event.currentTarget;
        resetHomeActionButtons(doc, win);
        clearPendingDuplicate();

        let metadataDocument;
        try { metadataDocument = state.shell?.getDocument();
            if (!metadataDocument) throw new Error('元数据字段尚未就绪。');
        } catch (error) { showFeedback(error.message, 'error'); return; }
        const mode = getSelectedMode(doc);
        if (mode === 'upload') {
            await submitUpload(metadataDocument);
            return;
        }

        const formPayload = { kind: mode, url: readOptionalField(form, 'url'), force: readForceValue(form), metadata_document: JSON.stringify(metadataDocument) };
        const url = (formPayload.url || '').trim();
        if (!url) {
            showFeedback('请先输入 Telegraph 链接。', 'error');
            return;
        }

        setSubmitting(true);
        showFeedback('正在提交任务...', 'info');

        try {
            const { response, payload } = await postDownloadRequest(formPayload, controller);
            if (!isCurrentPage(controller)) return;
            if (!response.ok || !payload || payload.ok !== true) {
                showFeedback(buildRequestErrorMessage(payload, response.status), 'error');
                return;
            }

            handleDownloadSuccess(payload, formPayload);
        } catch (error) {
            if (!isCurrentPage(controller)) return;
            console.error('Failed to submit download:', error);
            showFeedback('网络异常，请稍后重试。', 'error');
            resetHomeActionButtons(doc, win);
        } finally {
            if (isCurrentPage(controller)) setSubmitting(false);
        }
    }

    function clearTimers() {
        if (state.summaryTimer) {
            win.clearTimeout(state.summaryTimer);
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
        const root = form || summaryPanel;
        if (state.mountedRoot === root) return;
        if (state.mountedRoot) unmount();
        state.mountedRoot = root;
        state.pageController = new AbortController();
        state.summaryFailures = 0;

        if (form) {
            state.form = form;
            state.submitHandler = submitForm;
            form.addEventListener('submit', state.submitHandler);
            state.inputModeNodes = Array.from(form.querySelectorAll('input[name="input_mode"]'));
            state.inputModeHandler = () => applyInputMode(doc, getSelectedMode(doc));
            state.inputModeNodes.forEach((node) => node.addEventListener('change', state.inputModeHandler));
            applyInputMode(doc, getSelectedMode(doc));
            state.hintCleanup = bindFieldHintToggles(doc);
            const workspace = doc.getElementById('home-page');
            state.evidenceTabs = createTabs({ root: workspace?.querySelector('[data-evidence-tabs]'), win, defaultTab: 'ocr' });
            state.evidenceTabs.mount();
            state.shell = createWorkspaceShell({ root: workspace, doc, adapters: {
                async loadSchema(options) {
                    const { response, payload } = await api.getJson('/api/metadata/schema', options);
                    if (!response.ok) throw new Error('元数据字段读取失败，请重试。');
                    return payload;
                },
                onReady({draft,schema}) {
                    state.ocr?.dispose();
                    state.metadataSearch?.unmount();
                    const onInputChange = () => {
                        const context = draft.getContext();
                        draft.setContext({...context,inputRevision:context.inputRevision + 1});
                        state.shell?.showCandidate(null);
                        return draft.getContext();
                    };
                    const setConfigRevision = configRevision => {
                        draft.setContext({...draft.getContext(),configRevision});
                        state.shell?.showCandidate(null);
                    };
                    const evidence = workspace.querySelector('[data-module-slot="evidence"]');
                    state.ocr = evidence && mountOCR(evidence,{draft,api,schema,onInputChange,setConfigRevision,onCandidates:(candidates,options) => state.shell?.showCandidate(candidates[0] || null,options)});
                    state.metadataSearch = createMetadataSearchModule({root:workspace.querySelector('[data-module-slot="metadata-search"]'),doc,api,getDraft:()=>draft,getRegistry:()=>schema,showCandidate:(candidate,options)=>state.shell?.showCandidate(candidate,options),onInputChange});
                    state.metadataSearch.mount();
                },
            } });
            state.shell.mount();
            state.retryNode = workspace?.querySelector('[data-metadata-retry]');
            state.retryHandler = () => retryMetadataWorkspace(state, showFeedback);
            state.retryNode?.addEventListener('click', state.retryHandler);
            fetchMetadataHistory();
            resetHomeActionButtons(doc, win);
            clearPendingDuplicate();
            setSubmitting(false);
        } else {
            state.form = null;
            state.submitHandler = null;
            state.inputModeNodes = [];
            state.inputModeHandler = null;
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
            state.visibilityHandler = () => {
                clearTimers();
                if (doc.visibilityState !== 'hidden') fetchSummary();
            };
            doc.addEventListener('visibilitychange', state.visibilityHandler);
        } else {
            startupRecoveryBanner.hide();
        }
    }

    function unmount() {
        state.evidenceTabs?.unmount();
        state.evidenceTabs = null;
        state.ocr?.dispose();
        state.ocr = null;
        state.metadataSearch?.unmount();
        state.metadataSearch = null;
        state.historyController?.abort();
        state.historyController = null;
        state.shell?.unmount();
        state.shell = null;
        state.retryNode?.removeEventListener('click', state.retryHandler);
        state.retryNode = null;
        state.retryHandler = null;
        doc.querySelector('[data-history-preview]')?.replaceChildren();
        doc.getElementById('metadata-history-list')?.replaceChildren();
        state.mountedRoot = null;
        if (state.pageController) state.pageController.abort();
        state.pageController = null;
        if (state.visibilityHandler) doc.removeEventListener('visibilitychange', state.visibilityHandler);
        state.visibilityHandler = null;
        const startupRecoveryDismissBtn = doc.getElementById('startup-recovery-dismiss-home');
        if (state.form && state.submitHandler) {
            state.form.removeEventListener('submit', state.submitHandler);
        }
        if (state.inputModeNodes.length && state.inputModeHandler) {
            state.inputModeNodes.forEach((node) => node.removeEventListener('change', state.inputModeHandler));
        }
        if (startupRecoveryDismissBtn && state.startupRecoveryDismissHandler) {
            startupRecoveryDismissBtn.removeEventListener('click', state.startupRecoveryDismissHandler);
        }
        if (typeof state.hintCleanup === 'function') {
            state.hintCleanup();
            state.hintCleanup = null;
        }
        state.form = null;
        state.submitHandler = null;
        state.inputModeNodes = [];
        state.inputModeHandler = null;
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
        resetHomeActionButtons(doc, win);
        clearPendingDuplicate();
        clearTimers();
    }

    return {
        mount,
        unmount,
        hasUnsavedChanges: () => Boolean(state.shell?.hasUnsavedChanges() || state.ocr?.isDirty()),
        canLeave: () => !(state.shell?.hasUnsavedChanges() || state.ocr?.isDirty()) || win.confirm('当前元数据或识别文字尚未提交，确定离开并放弃修改吗？'),
    };
}
