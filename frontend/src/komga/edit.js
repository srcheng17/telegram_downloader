import { createMetadataDraft } from '../shared/metadata/draft.js';
import { decodeMetadataSchema, formatFieldValue, parseFieldInput } from '../shared/metadata/schema.js';
import { messageForFailure } from './messages.js';

const fieldReasons = Object.freeze({
    internal_only: '此字段仅在应用内保存，不能写回 ComicInfo。',
    series_scope: '此字段可能影响系列元数据，首版单书编辑暂不支持写回。',
    field_disabled: '此字段当前未启用或不允许编辑。',
    book_read_only: '此作品当前只读，不能修改元数据。',
    derived_page_count: '页数由归档中的页面决定，不能作为普通字段修改。',
    komga_field_locked: '此字段已在 Komga 锁定，请先在 Komga 中调整。',
    barcode_isbn_clear_disabled: '书库启用了条码 ISBN 导入，不能清空 ISBN。',
    no_clear: '此字段不能清空。',
});

const blockReasons = Object.freeze({
    unsupported_format: '只有 CBZ 文件支持写回。',
    unmapped_library: '此书库未配置可写共享挂载。',
    library_unavailable: '书库当前不可用。',
    comicinfo_import_disabled: '书库尚未启用 ComicInfo 导入。',
    file_unavailable: '无法安全打开此文件。',
    unsafe_archive: '归档结构无法保证保真，已停止写回。',
    unsafe_comicinfo: '原 ComicInfo 无法安全保留，已停止写回。',
    media_not_ready: 'Komga 尚未完成此作品的分析，请稍后重试。',
    backup_unavailable: '私密原件备份目录不可用，已停止写回。',
    source_changed: '原文件已变化，请重新读取后核对。',
});

function knownReason(code, table, fallback) {
    return typeof code === 'string' && Object.hasOwn(table, code) ? table[code] : fallback;
}

function createElement(doc, tag, text, className = '') {
    const node = doc.createElement(tag);
    if (text !== undefined) node.textContent = text;
    if (className) node.className = className;
    return node;
}

function describeValue(definition, value) {
    if (value === null || value === undefined) return '未填写';
    try { return formatFieldValue(definition, value) || '未填写'; }
    catch { return '无法显示'; }
}

function showWarnings(doc, host, warnings) {
    host.replaceChildren();
    if (!Array.isArray(warnings)) return;
    for (const warning of warnings.slice(0, 30)) {
        const text = typeof warning?.message === 'string' && warning.message.length <= 1000 ? warning.message : '此字段需要进一步核对。';
        host.appendChild(createElement(doc, 'p', text, 'komga-edit-warning'));
    }
}

function validOperation(value, bookID) {
    return value && typeof value === 'object' && typeof value.id === 'string' && value.id.length > 0 && value.book_id === bookID &&
        typeof value.state === 'string' && typeof value.file_committed === 'boolean' && typeof value.file_no_change === 'boolean' &&
        typeof value.file_restored === 'boolean' &&
        typeof value.projection_applicable === 'boolean' && typeof value.projection_consistent === 'boolean' &&
        typeof value.analyze_verified === 'boolean' && Array.isArray(value.available_actions);
}

// The editor owns only one selected book. The catalog owns selection and
// disposes this controller before another book replaces its DOM.
export function createKomgaEditor({ win, doc, api, book, container, signal }) {
    let live = true;
    let draft;
    let schema;
    let detail;
    let sourceVersion = '';
    let cleanRevision = 0;
    let sourceStale = false;
    let previewResult;
    let previewInput;
    let previewRevision = -1;
    let pendingSave;
    let operation;
    let loadVersion = 0;
    let loadController;
    let previewController;
    let mutationController;
    let status;
    let form;
    let previewButton;
    let correctionInput;
    let saveButton;
    let retrySaveButton;
    let reloadButton;
    let previewPanel;
    let warningsPanel;
    let operationPanel;
    const controls = new Map();
    const cleanup = [];
    const host = createElement(doc, 'section', undefined, 'komga-file-editor');
    host.setAttribute('aria-label', 'CBZ 文件元数据');
    container.appendChild(host);

    function listen(node, kind, handler) {
        node.addEventListener(kind, handler);
        cleanup.push(() => node.removeEventListener(kind, handler));
    }
    function say(text, state = 'info') {
        if (!live || !status) return;
        status.textContent = text;
        status.dataset.state = state;
    }
    function current(controller) { return live && !signal?.aborted && !controller.signal.aborted; }
    function hasUnsavedChanges() { return Boolean(pendingSave || correctionInput?.checked || draft?.isDirty() || [...controls.values()].some(control => control.invalid)); }
    function canLeave(confirmDiscard) { return !hasUnsavedChanges() || Boolean(confirmDiscard?.()); }
    function invalidatePreview() {
        previewController?.abort();
        previewController = null;
        previewResult = null;
        previewInput = null;
        previewRevision = -1;
        if (saveButton) saveButton.disabled = true;
        previewPanel?.replaceChildren();
    }
    function changes() {
        const snapshot = draft.getSnapshot();
        return detail.fields.filter(field => field.can_set || field.can_clear).flatMap(field => {
            const changed = snapshot.fields[field.key];
            if (!changed || changed.revision <= cleanRevision) return [];
            if (changed.state === 'cleared' && field.can_clear) return [{ key: field.key, state: 'cleared' }];
            if (changed.state === 'value' && field.can_set) return [{ key: field.key, state: 'value', value: changed.value }];
            return [];
        });
    }
    function setValidity(item, error) {
        item.invalid = Boolean(error);
        item.error.textContent = error || '';
        item.error.hidden = !error;
        item.input.setAttribute('aria-invalid', error ? 'true' : 'false');
        draft.setInputValidity(item.key, !error);
    }
    function changeField(item) {
        if (!live || !draft) return;
        invalidatePreview();
        try {
            draft.setField(item.key, parseFieldInput(item.definition, item.input.value));
            setValidity(item, '');
            say('草稿已修改，请预览写回差异。');
        } catch (error) {
            setValidity(item, error?.message || '字段输入无效。');
            say('请先修正标记的字段。', 'error');
        }
    }
    function clearField(item) {
        if (!live || !draft) return;
        invalidatePreview();
        try {
            draft.clearField(item.key);
            item.input.value = '';
            setValidity(item, '');
            say('已明确选择清空此字段，请预览写回差异。');
        } catch {
            say('此字段不能清空。', 'error');
        }
    }

    function renderField(field) {
        const definition = schema.definitions[field.key];
        const wrapper = createElement(doc, 'div', undefined, 'komga-edit-field');
        wrapper.setAttribute('data-field-key', field.key);
        const label = createElement(doc, 'label', field.label || definition?.label || field.key);
        const inputID = `komga-edit-${field.key.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
        label.htmlFor = inputID;
        wrapper.appendChild(label);
        if (!definition || !field.can_set && !field.can_clear) {
            const reason = knownReason(field.reason, fieldReasons, '此字段当前只读。');
            const prior = detail.document.fields?.[field.key];
            const value = prior?.state === 'cleared' ? '已清空' : describeValue(definition, prior?.value);
            wrapper.appendChild(createElement(doc, 'p', value, 'komga-edit-value'));
            wrapper.appendChild(createElement(doc, 'p', reason, 'field-help'));
            return wrapper;
        }
        let input;
        if (definition.type === 'string[]' || definition.type === 'identifiers' || field.key === 'summary') {
            input = createElement(doc, 'textarea');
            input.rows = field.key === 'summary' ? 4 : 2;
        } else if (definition.type === 'boolean' || Array.isArray(definition.enum)) {
            input = createElement(doc, 'select');
            const options = definition.type === 'boolean' ? [['', '未填写'], ['true', '是'], ['false', '否']] : [['', '未填写'], ...definition.enum.map(value => [value, value])];
            for (const [value, text] of options) { const option = createElement(doc, 'option', text); option.value = value; input.appendChild(option); }
        } else {
            input = createElement(doc, 'input');
            input.type = definition.type === 'integer' ? 'number' : 'text';
            if (definition.type === 'integer') input.step = '1';
            if (definition.type === 'date') input.placeholder = '例如 2024-03-15';
        }
        input.id = inputID;
        input.name = `comicinfo.${field.key}`;
        input.disabled = !field.can_set || !detail.can_save;
        const initial = detail.document.fields?.[field.key];
        input.value = initial?.state === 'value' ? formatFieldValue(definition, initial.value) : '';
        const error = createElement(doc, 'p', '', 'field-error');
        error.id = `${inputID}-error`;
        error.hidden = true;
        input.setAttribute('aria-describedby', error.id);
        input.setAttribute('aria-invalid', 'false');
        wrapper.appendChild(input);
        wrapper.appendChild(error);
        const item = { key: field.key, input, error, definition, invalid: false };
        controls.set(field.key, item);
        if (field.can_set) listen(input, 'input', () => changeField(item));
        if (field.can_clear) {
            const clear = createElement(doc, 'button', '明确清空', 'text-button');
            clear.type = 'button';
            clear.disabled = !detail.can_save;
            clear.setAttribute('aria-label', `明确清空${field.label || field.key}`);
            listen(clear, 'click', () => clearField(item));
            wrapper.appendChild(clear);
        } else {
            wrapper.appendChild(createElement(doc, 'p', knownReason(field.reason, fieldReasons, '此字段不支持清空。'), 'field-help'));
        }
        return wrapper;
    }

    function renderPreview(result) {
        previewPanel.replaceChildren();
        previewPanel.appendChild(createElement(doc, 'h3', '写回预览'));
        const diffs = Array.isArray(result.diffs) ? result.diffs : [];
        if (!diffs.length) previewPanel.appendChild(createElement(doc, 'p', 'ComicInfo 中没有文件差异；仍会核对 Komga 同步状态。'));
        for (const diff of diffs.slice(0, 64)) {
            const definition = schema.definitions[diff?.key];
            const label = definition?.label || diff?.key || '字段';
            const line = createElement(doc, 'div', undefined, 'komga-preview-diff');
            line.appendChild(createElement(doc, 'strong', label));
            line.appendChild(createElement(doc, 'span', `${describeValue(definition, diff.before)} → ${diff.action === 'clear' ? '明确清空' : describeValue(definition, diff.after)}`));
            previewPanel.appendChild(line);
        }
        showWarnings(doc, warningsPanel, result.warnings);
        if (!result.can_save) previewPanel.appendChild(createElement(doc, 'p', knownReason(result.block_reason, blockReasons, '当前无法安全保存，请核对提示后重试。'), 'field-error'));
    }

    async function preview() {
        if (!live || !draft || !detail?.can_save || sourceStale || mutationController || operation?.state === 'restore_needed') return;
        if (pendingSave) { say('先重试确认上一笔保存操作，再预览新的修改。', 'error'); return; }
        if (hasUnsavedChanges() && [...controls.values()].some(control => control.invalid)) { say('请先修正标记的字段。', 'error'); return; }
        const selectedChanges = changes();
        if (!selectedChanges.length && !correctionInput?.checked) { say('请先修改或明确清空至少一个字段。'); return; }
        invalidatePreview();
        const input = { source_version: sourceVersion, definitions_version: detail.document.definitions_version, changes: selectedChanges };
        if (correctionInput?.checked) input.correct_page_count = true;
        const revision = draft.getSnapshot().revision;
        const controller = new AbortController();
        previewController = controller;
        previewButton.disabled = true;
        say('正在验证归档并生成写回预览…', 'loading');
        try {
            const result = await api.preview(book.id, input, { signal: controller.signal });
            if (!current(controller) || previewController !== controller || draft.getSnapshot().revision !== revision) return;
            if (!result?.response?.ok) { say(messageForFailure(result, '写回预览失败，请稍后重试。'), 'error'); return; }
            const value = result.payload?.preview || result.payload;
            if (!value || typeof value.can_save !== 'boolean' || !Array.isArray(value.diffs)) { say('写回预览格式无效，已停止保存。', 'error'); return; }
            previewResult = value;
            previewInput = input;
            previewRevision = revision;
            renderPreview(value);
            saveButton.disabled = !value.can_save || typeof value.preview_token !== 'string' || !value.preview_token;
            say(value.can_save ? '请核对差异，确认后再写回 CBZ。' : knownReason(value.block_reason, blockReasons, '当前无法安全保存，请核对预览提示。'), value.can_save ? 'ready' : 'error');
        } catch {
            if (current(controller) && previewController === controller) say('写回预览请求失败，请稍后重试。', 'error');
        } finally {
            if (previewController === controller) { previewController = null; if (live) previewButton.disabled = false; }
        }
    }

    function renderOperation(value) {
        operationPanel.replaceChildren();
        operationPanel.appendChild(createElement(doc, 'h3', '保存与同步状态'));
        const list = createElement(doc, 'ul');
        list.appendChild(createElement(doc, 'li', value.state === 'restore_needed' ? 'CBZ 文件状态无法安全判定。' : value.file_restored ? 'CBZ 已恢复原件。' :
            value.file_committed ? 'CBZ 内 ComicInfo 已保存。' : value.file_no_change ? 'CBZ 内 ComicInfo 无需改写。' : 'CBZ 文件尚未确认写入。'));
        list.appendChild(createElement(doc, 'li', !value.projection_applicable ? '本次修改没有可核对的 Komga 单书字段。' :
            value.projection_consistent ? 'Komga 当前元数据值已一致。' : 'Komga 当前元数据仍待同步或核对。'));
        list.appendChild(createElement(doc, 'li', value.analyze_verified ? '已取得本次单书分析的验证证据。' : '本次单书分析尚无法证明完成。'));
        operationPanel.appendChild(list);
        if (value.state === 'restore_needed') operationPanel.appendChild(createElement(doc, 'p',
            `操作编号：${value.id}。请人工核对私密备份和当前 CBZ；此作品暂不能继续保存。`, 'komga-edit-warning'));
        if (value.last_error_code) operationPanel.appendChild(createElement(doc, 'p', '后续同步遇到问题，可按可用操作重试。', 'komga-edit-warning'));
        const actions = createElement(doc, 'div', undefined, 'komga-operation-actions');
        const refresh = createElement(doc, 'button', '刷新状态'); refresh.type = 'button';
        refresh.onclick = refreshOperation; actions.appendChild(refresh);
        if (value.available_actions.includes('retry_sync')) {
            const retry = createElement(doc, 'button', '重试 Komga 同步'); retry.type = 'button';
            retry.onclick = retrySync; actions.appendChild(retry);
        }
        if (value.available_actions.includes('restore')) {
            const restore = createElement(doc, 'button', '恢复原件'); restore.type = 'button';
            restore.onclick = restoreOriginal; actions.appendChild(restore);
        }
        operationPanel.appendChild(actions);
    }

    function receiveOperation(result) {
        const value = result?.payload?.operation || result?.payload;
        if (!validOperation(value, book.id)) { say('操作状态格式无效，请稍后刷新。', 'error'); return false; }
        operation = value;
        renderOperation(value);
        return true;
    }

    function settlePendingSave() {
        if (!pendingSave) return 'none';
        if (operation?.state === 'aborted' || operation?.file_restored || operation?.state === 'restore_needed') {
            const restored = operation.file_restored;
            const restoreNeeded = operation.state === 'restore_needed';
            pendingSave = null;
            invalidatePreview();
            retrySaveButton.hidden = true;
            if (restored || restoreNeeded) {
                sourceStale = true;
                previewButton.disabled = true;
            }
            return restoreNeeded ? 'restore_needed' : restored ? 'restored' : 'aborted';
        }
        if (!operation?.file_committed && !operation?.file_no_change) {
            retrySaveButton.hidden = false;
            return 'pending';
        }
        const submittedRevision = pendingSave.revision;
        pendingSave = null;
        cleanRevision = submittedRevision;
        draft.markClean(submittedRevision);
        if (correctionInput) correctionInput.checked = false;
        sourceStale = true;
        invalidatePreview();
        previewButton.disabled = true;
        retrySaveButton.hidden = true;
        return 'saved';
    }

    function save() {
        if (!live || !draft || pendingSave || !previewResult?.can_save || !previewInput || sourceStale || mutationController || operation?.state === 'restore_needed' ||
            draft.getSnapshot().revision !== previewRevision || [...controls.values()].some(control => control.invalid)) return;
        const key = win.crypto?.randomUUID?.() || '';
        if (!key) { say('浏览器无法生成安全操作编号，已停止保存。', 'error'); return; }
        pendingSave = { input: { ...previewInput, preview_token: previewResult.preview_token, idempotency_key: key }, revision: previewRevision };
        submitPendingSave();
    }

    async function submitPendingSave() {
        if (!live || !pendingSave || mutationController) return;
        const controller = new AbortController();
        mutationController = controller;
        const attempt = pendingSave;
        saveButton.disabled = true;
        retrySaveButton.hidden = true;
        say('正在备份原件并写回 CBZ，请勿关闭页面…', 'loading');
        try {
            const result = await api.save(book.id, attempt.input, { signal: controller.signal });
            if (!current(controller) || mutationController !== controller) return;
            if (!result?.response?.ok) { retrySaveButton.hidden = false; say(messageForFailure(result, '保存结果未能确认，请重试确认同一操作。'), 'error'); return; }
            if (!receiveOperation(result)) { retrySaveButton.hidden = false; return; }
            const outcome = settlePendingSave();
            if (outcome === 'saved') {
                say(draft.isDirty() ? '文件已保存提交时的修订；随后修改的草稿仍保留。请重新读取文件后核对。' : '文件已处理；请分别核对 Komga 当前值和分析验证状态。', 'ready');
            } else if (outcome === 'aborted') say('保存操作在写回前终止；草稿仍保留，可重新预览后再保存。', 'error');
            else if (outcome === 'restored') say('此操作的 CBZ 已恢复原件；草稿仍保留，请重新读取文件后核对。', 'info');
            else if (outcome === 'restore_needed') say('文件状态无法安全判定。请记录操作编号并人工核对备份与当前 CBZ；草稿仍保留，可明确放弃后重新读取。', 'error');
            else say('保存操作已登记，文件结果尚未确认；可刷新状态或重试确认同一操作。', 'info');
        } catch {
            if (current(controller) && mutationController === controller) { retrySaveButton.hidden = false; say('保存响应未能确认。当前草稿和同一操作编号已保留，可重试确认。', 'error'); }
        } finally {
            if (mutationController === controller) { mutationController = null; if (live && !pendingSave && previewResult?.can_save && !sourceStale) saveButton.disabled = false; }
        }
    }

    async function operationAction(action, label) {
        if (!live || !operation || mutationController) return;
        const controller = new AbortController();
        mutationController = controller;
        say(label, 'loading');
        try {
            const result = action === 'refresh' ? await api.operation(operation.id, { signal: controller.signal }) : action === 'retry_sync' ? await api.retrySync(operation.id, { signal: controller.signal }) : await api.restore(operation.id, { signal: controller.signal });
            if (!current(controller) || mutationController !== controller) return;
            if (!result?.response?.ok) { say(messageForFailure(result, '操作失败，请稍后重试。'), 'error'); return; }
            if (receiveOperation(result)) {
                const outcome = settlePendingSave();
                if (action === 'restore') { sourceStale = true; previewButton.disabled = true; }
                if (outcome === 'aborted') say('保存操作在写回前终止；草稿仍保留，可重新预览后再保存。', 'error');
                else if (outcome === 'restored') say('此操作的 CBZ 已恢复原件；草稿仍保留，请重新读取文件后核对。', 'info');
                else if (outcome === 'restore_needed') say('文件状态无法安全判定。请记录操作编号并人工核对备份与当前 CBZ；草稿仍保留，可明确放弃后重新读取。', 'error');
                else say(outcome === 'saved' && draft.isDirty() ? '文件已保存提交时的修订；随后修改的草稿仍保留。请重新读取文件后核对。' : '已更新操作状态；请核对文件、Komga 值和分析证据。', 'ready');
            }
        } catch {
            if (current(controller) && mutationController === controller) say('无法获取最新操作状态，请稍后重试。', 'error');
        } finally {
            if (mutationController === controller) mutationController = null;
        }
    }
    function refreshOperation() { operationAction('refresh', '正在读取操作状态…'); }
    function retrySync() { if (operation?.available_actions?.includes('retry_sync')) operationAction('retry_sync', '正在重试 Komga 同步…'); }
    function restoreOriginal() {
        if (!operation?.available_actions?.includes('restore')) return;
        if (!win.confirm?.('恢复原件会撤销本次 CBZ 文件修改。服务器会先保护当前版本，确定继续吗？')) return;
        operationAction('restore', '正在验证备份并恢复原件…');
    }

    function reloadFile() {
        if (!live || mutationController) return;
        if (pendingSave) { say('先重试确认上一笔保存操作，再重新读取文件。', 'error'); return; }
        if (hasUnsavedChanges() && !win.confirm?.('重新读取文件会放弃当前未保存的元数据草稿，确定继续吗？')) return;
        previewController?.abort();
        cleanup.splice(0).forEach(remove => remove());
        draft?.dispose();
        draft = null;
        correctionInput = null;
        controls.clear();
        invalidatePreview();
        sourceStale = operation?.state === 'restore_needed';
        host.replaceChildren();
        status = createElement(doc, 'p', '正在重新读取文件…', 'inline-feedback');
        status.setAttribute('role', 'status');
        status.setAttribute('aria-live', 'polite');
        host.appendChild(status);
        load();
    }

    function renderEditor() {
        host.replaceChildren();
        host.appendChild(createElement(doc, 'h3', '文件内 ComicInfo'));
        host.appendChild(createElement(doc, 'p', detail.has_comicinfo ? `已读取原文件 ComicInfo · ${detail.page_count} 页` : `原文件没有 ComicInfo · ${detail.page_count} 页`, 'komga-detail-note'));
        status = createElement(doc, 'p', '', 'inline-feedback');
        status.setAttribute('role', 'status');
        status.setAttribute('aria-live', 'polite');
        host.appendChild(status);
        warningsPanel = createElement(doc, 'div', undefined, 'komga-edit-warnings');
        host.appendChild(warningsPanel);
        showWarnings(doc, warningsPanel, detail.warnings);
        if (!detail.can_save) {
            say(knownReason(detail.block_reason, blockReasons, '此作品当前不能安全写回，原文件保持不变。'), 'error');
        }
        form = createElement(doc, 'form', undefined, 'komga-edit-form');
        form.setAttribute('autocomplete', 'off');
        if (detail.page_count_correction_needed) {
            const wrapper = createElement(doc, 'div', undefined, 'komga-page-correction');
            const label = createElement(doc, 'label');
            correctionInput = createElement(doc, 'input');
            correctionInput.type = 'checkbox';
            correctionInput.id = 'komga-correct-page-count';
            correctionInput.disabled = !detail.can_save;
            label.appendChild(correctionInput);
            label.appendChild(createElement(doc, 'span', `确认将 ComicInfo 页数修正为实际图片数 ${detail.page_count} 页`));
            wrapper.appendChild(label);
            wrapper.appendChild(createElement(doc, 'p', '此修正会改变原文件的 PageCount，请核对后再预览。', 'field-help'));
            listen(correctionInput, 'change', () => {
                invalidatePreview();
                previewButton.disabled = !detail.can_save || !correctionInput.checked || sourceStale;
                say(correctionInput.checked ? '已确认页数修正，请预览写回差异。' : '请先确认页数修正。');
            });
            form.appendChild(wrapper);
        }
        for (const field of detail.fields) form.appendChild(renderField(field));
        previewButton = createElement(doc, 'button', '预览写回差异'); previewButton.type = 'submit'; previewButton.disabled = !detail.can_save || Boolean(detail.page_count_correction_needed) || sourceStale;
        saveButton = createElement(doc, 'button', '确认保存到 CBZ'); saveButton.type = 'button'; saveButton.disabled = true;
        retrySaveButton = createElement(doc, 'button', '重试确认同一保存操作'); retrySaveButton.type = 'button'; retrySaveButton.hidden = true;
        reloadButton = createElement(doc, 'button', '重新读取文件', 'text-button'); reloadButton.type = 'button';
        listen(form, 'submit', event => { event.preventDefault(); preview(); });
        listen(saveButton, 'click', save);
        listen(retrySaveButton, 'click', submitPendingSave);
        listen(reloadButton, 'click', reloadFile);
        form.appendChild(previewButton);
        form.appendChild(saveButton);
        form.appendChild(retrySaveButton);
        form.appendChild(reloadButton);
        host.appendChild(form);
        previewPanel = createElement(doc, 'section', undefined, 'komga-preview-panel');
        host.appendChild(previewPanel);
        operationPanel = createElement(doc, 'section', undefined, 'komga-operation-panel');
        host.appendChild(operationPanel);
        if (operation) renderOperation(operation);
    }

    async function load() {
        const ticket = ++loadVersion;
        loadController?.abort();
        const controller = new AbortController();
        loadController = controller;
        say('正在读取 CBZ 文件元数据…', 'loading');
        try {
            const result = await api.edit(book.id, { signal: controller.signal });
            if (!live || signal?.aborted || controller.signal.aborted || ticket !== loadVersion) return;
            if (!result?.response?.ok) { say(messageForFailure(result, '无法读取文件内元数据。'), 'error'); return; }
            const payload = result.payload?.detail || result.payload;
            if (!payload || payload.book?.id !== book.id || typeof payload.source_version !== 'string' || !payload.document || !Array.isArray(payload.fields) || payload.fields.length > 128 || typeof payload.can_save !== 'boolean' ||
                payload.page_count_correction_needed !== undefined && typeof payload.page_count_correction_needed !== 'boolean') {
                say('文件元数据响应格式无效，已停止编辑。', 'error'); return;
            }
            schema = decodeMetadataSchema(payload.schema);
            if (payload.document.definitions_version !== schema.definitions_version ||
                payload.fields.some(field => !field || typeof field.key !== 'string' || !field.key || typeof field.can_set !== 'boolean' || typeof field.can_clear !== 'boolean') ||
                new Set(payload.fields.map(field => field.key)).size !== payload.fields.length ||
                payload.can_save && !payload.source_version) {
                say('文件元数据与字段定义不一致，已停止编辑。', 'error'); return;
            }
            detail = payload;
            draft = createMetadataDraft({ document: payload.document, definitions: schema.definitions, definitionsVersion: schema.definitions_version, limits: schema.limits });
            sourceVersion = payload.source_version;
            cleanRevision = payload.document.revision;
            renderEditor();
            if (operation?.state === 'restore_needed') say('上次操作的文件状态仍需人工核对；此作品暂不能继续保存。', 'error');
            else if (detail.can_save) say('请修改字段并预览；未修改的 ComicInfo 字段将保持原值。', 'ready');
        } catch {
            if (live && !signal?.aborted && !controller.signal.aborted && ticket === loadVersion) say('无法读取文件内元数据，请稍后重新选择作品。', 'error');
        } finally {
            if (loadController === controller) loadController = null;
        }
    }

    status = createElement(doc, 'p', '正在读取 CBZ 文件元数据…', 'inline-feedback');
    status.setAttribute('role', 'status');
    status.setAttribute('aria-live', 'polite');
    host.appendChild(status);
    signal?.addEventListener('abort', dispose, { once: true });
    load();

    function dispose() {
        if (!live) return;
        live = false;
        loadVersion++;
        signal?.removeEventListener('abort', dispose);
        loadController?.abort();
        previewController?.abort();
        mutationController?.abort();
        cleanup.splice(0).forEach(remove => remove());
        draft?.dispose();
        draft = null;
        correctionInput = null;
        controls.clear();
        host.replaceChildren();
    }
    return { dispose, hasUnsavedChanges, canLeave };
}
