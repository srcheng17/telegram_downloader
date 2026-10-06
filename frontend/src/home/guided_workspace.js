import { createGuidedWorkflow } from './workflow.js';
import { createCurrentTask } from './current_task.js';
import { mapTaskToLogViewModel } from '../logs/view_model.js';
import { canDownloadTaskAction, canRetryTaskAction } from '../logs/task_actions.js';
import { getSelectedMode } from './input_mode.js';
import { readForceValue } from './state.js';

export function createGuidedWorkspace({ root, win, doc, api, getShell, getOCR, getSearch, showFeedback }) {
    const listeners = [];
    let warnings = [];
    let disposed = false;
    const target = root.querySelector('#delivery-target');
    const auto = root.querySelector('#automatic-preparation');
    const on = (selector, event, handler) => root.querySelectorAll(selector).forEach(node => {
        node.addEventListener(event, handler); listeners.push(() => node.removeEventListener(event, handler));
    });
    function renderWarnings() {
        const host = root.querySelector('[data-workflow-warnings]');
        if (host) {
            host.replaceChildren(); host.hidden = !warnings.length;
            for (const text of [...new Set(warnings)]) { const item = doc.createElement('p'); item.textContent = text; host.appendChild(item); }
        }
        const retry = root.querySelector('[data-workflow-retry]');
        if (retry) retry.hidden = !warnings.length || Boolean(workflow.taskId);
    }
    function renderSource() {
        const source = readSource(); const host = root.querySelector('[data-workflow-source]');
        if (host) host.textContent = source.mode === 'upload' ? (source.file ? `上传：${source.file.name}` : '尚未选择归档，请返回上一步补充。') : (source.url ? `Telegraph：${source.url}` : '尚未填写下载链接，请返回上一步补充。');
        const help = root.querySelector('[data-delivery-help]');
        if (help) help.textContent = target?.value === 'komga' ? '归档生成后自动复制并检查 Komga 收录。只有作品实际可见才显示已收录。' : '生成含作品信息的 CBZ，完成后在此下载。';
    }
    function renderStep(step, focus = true) {
        if (disposed) return;
        root.dataset.workflowStep = step;
        root.querySelectorAll('[data-workflow-step]').forEach(panel => { panel.hidden = panel.dataset.workflowStep !== step; panel.inert = panel.hidden; });
        root.querySelectorAll('[data-workflow-indicator]').forEach(item => {
            if (item.dataset.workflowIndicator === step) item.setAttribute('aria-current', 'step'); else item.removeAttribute('aria-current');
        });
        const history = root.querySelector('[data-workflow-history]'); if (history) history.hidden = step !== 'review' || Boolean(workflow.taskId);
        const submit = root.querySelector('button[type="submit"]'); if (submit) submit.hidden = Boolean(workflow.taskId);
        const result = root.querySelector('[data-workflow-result]'); if (result) result.hidden = !workflow.taskId;
        lock(workflow.busy || Boolean(workflow.taskId));
        renderSource(); renderWarnings();
        if (focus) root.querySelector(`#workflow-${step}-title`)?.focus();
    }
    function lock(value) {
        const editable = root.querySelector('[data-workflow-editable]'); if (editable) { editable.disabled = value; editable.inert = workflow.busy; }
        root.querySelectorAll('[data-workflow-back], [data-workflow-next], [data-workflow-retry]').forEach(button => { button.disabled = workflow.busy; });
    }
    async function prepare({ signal, retry }) {
        if (!getShell()?.getDraft()) throw new Error('作品字段尚未准备好，请稍后重试。');
        warnings = [];
        if (!auto?.checked) return;
        const progress = root.querySelector('[data-workflow-progress]');
        const collect = async entries => {
            if (signal.aborted || !entries?.length) return;
            const result = await getShell().prepareCandidates(entries, { signal });
            warnings.push(...(result?.warnings || []));
        };
        if (progress) progress.textContent = '正在识别文字并整理作品字段…';
        const recognition = await getOCR()?.prepare({ allowAI: true, signal, retry });
        if (signal.aborted) return;
        warnings.push(...(recognition?.warnings || [])); await collect(recognition?.entries);
        if (signal.aborted) return;
        if (progress) progress.textContent = '正在查询已启用的书目来源…';
        const fields = getShell().getDraft().getSnapshot().fields;
        const value = key => fields[key]?.state === 'value' ? fields[key].value : undefined;
        const search = await getSearch()?.prepare({ title: value('title'), aliases: value('aliases'), writers: value('creators.writer'), signal, retry });
        if (signal.aborted) return;
        warnings.push(...(search?.warnings || [])); await collect(search?.entries);
    }
    const workflow = createGuidedWorkflow({
        prepare,
        getPreparationKey: () => `${auto?.checked ? 1 : 0}:${getOCR()?.getInputRevision() ?? 0}`,
        onStep: renderStep,
        createKey: () => win.crypto.randomUUID(),
    });
    const tracker = createCurrentTask({ api, win, doc, onChange: renderTask });
    function renderTask(snapshot) {
        if (disposed) return;
        const host = root.querySelector('[data-task-result]'); if (!host) return;
        host.replaceChildren();
        const p = message => { const node = doc.createElement('p'); node.textContent = message; host.appendChild(node); };
        if (!snapshot.task) { p('正在读取当前任务…'); if (snapshot.error) p(snapshot.error); return; }
        const task = snapshot.task; const view = mapTaskToLogViewModel(task);
        p(`当前任务：${view.statusLabel || view.status}`); p(view.progressText);
        const total = Number(task.progress?.total || 0);
        if (total > 0) { const bar = doc.createElement('progress'); bar.max = total; bar.value = Number(task.progress?.current || 0); bar.setAttribute('aria-label', '当前任务进度'); host.appendChild(bar); }
        if (snapshot.target === 'komga') {
            const messages = { waiting: '正在生成归档，随后整理到 Komga。', copying: '归档已生成，正在整理并检查 Komga 收录…', pending: '已复制，等待 Komga 收录。', indexed: 'Komga 已收录，作品与元数据已读回确认。', unavailable: '当前任务暂不能交付到 Komga，可在任务页检查连接与可用操作。', failed: 'Komga 交付未完成。' };
            p(messages[snapshot.delivery.status] || '正在检查交付结果。');
        } else if (canDownloadTaskAction(task)) p('归档已生成，可下载 CBZ。');
        for (const warning of view.metadataWarnings) p(warning);
        if (snapshot.error) p(snapshot.error);
        if (view.errorText) p('作品处理失败。可重试当前任务，或在任务页查看详情。');
        const actions = doc.createElement('div'); actions.className = 'workflow-result-actions';
        if (canDownloadTaskAction(task)) { const link = doc.createElement('a'); link.className = 'button-link'; link.href = `/api/tasks/${encodeURIComponent(task.id)}/download`; link.textContent = '下载 CBZ'; actions.appendChild(link); }
        if (canRetryTaskAction(task) || ['pending', 'failed', 'unavailable'].includes(snapshot.delivery.status)) {
            const button = doc.createElement('button'); button.type = 'button'; button.className = 'btn-secondary'; button.disabled = snapshot.busy;
            button.textContent = canRetryTaskAction(task) ? '重试当前任务' : '重新检查 Komga 交付'; button.onclick = () => tracker.retry(); actions.appendChild(button);
        }
        host.appendChild(actions);
    }
    function readSource() {
        const mode = getSelectedMode(doc);
        const file = root.querySelector('#archive_file')?.files?.[0];
        return { mode, url: mode === 'url' ? String(root.querySelector('#url')?.value || '').trim() : '', file: mode === 'upload' ? file : undefined, force: readForceValue(root.querySelector('#download-form')) };
    }
    function readSnapshot() {
        const { file, ...source } = readSource();
        if (file) Object.assign(source, { name: file.name, size: file.size, lastModified: file.lastModified });
        return { document: getShell().getDocument(), context: getShell().getDraft().getContext(), source, target: target?.value === 'komga' ? 'komga' : 'download', file, ocrRevision: getOCR()?.getInputRevision() };
    }
    function focusProblem() {
        const invalid = root.querySelector('[aria-invalid="true"]');
        const control = invalid || root.querySelector('[data-review-field] button');
        if (control) { const group = control.closest?.('details'); if (group) group.open = true; control.focus(); }
    }
    async function next(options) {
        showFeedback('', '');
        try { await workflow.next(options); } catch (error) { showFeedback(error.message || '自动准备暂未完成，可关闭自动准备后手动填写。', 'error'); }
    }
    on('[data-workflow-next], [data-workflow-result]', 'click', () => next());
    on('[data-workflow-retry]', 'click', () => next({ retry: true }));
    on('[data-workflow-back]', 'click', () => { getOCR()?.cancelPreparation(); workflow.back(); });
    on('#delivery-target', 'change', renderSource);
    renderStep('materials', false);
    return {
        workflow,
        async loadDefaults(signal) {
            const initial = target?.value;
            try {
                const { response, payload } = await api.getJson('/v2/settings', { signal });
                if (!disposed && !signal.aborted && response.ok && target && target.value === initial && workflow.step === 'materials') target.value = payload?.download_action_mode === 'komga_copy' ? 'komga' : 'download';
            } catch { /* The visible default remains downloadable CBZ. */ }
        },
        async confirm(signal) {
            if (getShell()?.hasUnresolvedCandidates()) { focusProblem(); throw new Error('请先处理作品信息中的冲突，再确认开始。'); }
            let snapshot;
            try { snapshot = readSnapshot(); } catch (error) { focusProblem(); throw error; }
            if (snapshot.source.mode === 'upload') {
                if (!snapshot.file) throw new Error('请返回上一步选择压缩包文件。');
                if (!/\.(zip|cbz|rar|7z)$/i.test(snapshot.file.name) || snapshot.file.size <= 0 || snapshot.file.size > 64 * 1024 * 1024) throw new Error('请选择不超过 64 MiB 的 ZIP、CBZ、RAR 或 7Z 文件。');
            } else {
                let url; try { url = new URL(snapshot.source.url); } catch { throw new Error('请返回上一步输入有效的 Telegraph 链接。'); }
                if (!['http:', 'https:'].includes(url.protocol) || !['telegra.ph', 'www.telegra.ph', 'graph.org', 'www.graph.org'].includes(url.hostname)) throw new Error('下载来源只支持 Telegraph 链接。');
            }
            const attempt = workflow.confirm(snapshot); lock(true); getOCR()?.cancelPreparation();
            try {
                const { response, payload } = await api.getJson('/api/metadata/schema', { signal });
                if (!response.ok || payload?.definitions_version !== snapshot.document.definitions_version) throw new Error('作品字段设置已变化，请重新核对。');
                try { await getShell().preflightPreparedCandidates?.({ signal }); }
                catch {
                    warnings.push('识别或来源设置已变化，或校验暂不可用。请重新准备并核对，手工修改仍保留。'); renderWarnings();
                    throw new Error('建议的设置校验未通过，请重新准备并核对后开始。');
                }
                if (snapshot.file && !attempt.fileSHA256) {
                    const digest = await win.crypto.subtle.digest('SHA-256', await snapshot.file.arrayBuffer());
                    attempt.fileSHA256 = Array.from(new Uint8Array(digest), value => value.toString(16).padStart(2, '0')).join('');
                }
                if (signal.aborted || !workflow.matches(readSnapshot())) throw new Error('待提交内容已变化，请重新核对后开始。');
                return attempt;
            } catch (error) { workflow.failed(); lock(false); throw error; }
        },
        failed() { workflow.failed(); lock(false); },
        submitted(taskId) {
            const snapshot = workflow.submission?.snapshot;
            if (snapshot?.ocrRevision === getOCR()?.getInputRevision()) getOCR()?.markClean();
            workflow.complete(taskId); tracker.start(taskId, snapshot?.target || 'download');
        },
        dispose() { disposed = true; workflow.dispose(); tracker.stop(); listeners.forEach(remove => remove()); root.querySelector('[data-task-result]')?.replaceChildren(); },
    };
}
