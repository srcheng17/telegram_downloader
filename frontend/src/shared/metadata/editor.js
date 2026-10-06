import { formatFieldValue, parseFieldInput } from './schema.js';
import { createCandidateReview } from './candidate_review.js';

function groupFor(key) {
    if (key.startsWith('custom.')) return '自定义字段';
    if (key === 'creators.writer') return '作品信息';
    if (key.startsWith('creators.')) return '创作者';
    if (['summary', 'tags', 'genres', 'aliases'].includes(key)) return '简介与分类';
    if (['publisher', 'imprint', 'publication_date', 'language', 'format', 'count', 'volume', 'page_count', 'reading_direction', 'manga', 'age_rating'].includes(key)) return '出版与阅读';
    if (['identifiers', 'web'].includes(key)) return '标识符与来源';
    return '作品信息';
}
const groupOrder = ['作品信息', '简介与分类', '创作者', '出版与阅读', '标识符与来源', '自定义字段'];
const fieldOrder = ['title', 'creators.writer', 'series', 'number', 'summary', 'tags', 'genres', 'aliases'];
const sourceLabels = { manual: '手工填写', archive: '原归档', ocr: '截图识别', rule: '规则提取', ai: 'AI 建议', provider: '书目来源', legacy: '历史记录' };

export function createMetadataEditor({ root, draft, schema, doc = globalThis.document }) {
    const controls = new Map();
    const groupSummaries = new Map();
    const cleanup = [];
    let mounted = false;
    let pendingEdits = false;
    let unsubscribe;
    let candidatePanel;
    let candidateMode = null;
    let candidateGeneration = 0;
    let adoptionController;
    let feedback;
    let preparedReview;
    const el = (tag, className, text) => {
        const node = doc.createElement(tag);
        if (className) node.className = className;
        if (text !== undefined) node.textContent = text;
        return node;
    };
    function listen(node, type, listener) {
        node.addEventListener(type, listener);
        cleanup.push(() => node.removeEventListener(type, listener));
    }
    function sync(document) {
        for (const [key, item] of controls) {
            const field = document.fields[key];
            if (doc.activeElement !== item.control) item.control.value = field?.state === 'value' ? formatFieldValue(item.definition, field.value) : '';
            item.state.textContent = field?.state === 'cleared' ? '已明确清空' : field?.manual_locked ? '手工修改已保护' : field?.provenance?.length ? [...new Set(field.provenance.map(source => sourceLabels[source.kind] || '已保存来源'))].join('、') : '';
            item.state.hidden = !item.state.textContent;
            item.unlock.hidden = !field?.manual_locked || item.readOnly;
        }
        syncGroupSummaries(document);
    }
    function syncGroupSummaries(document) {
        for (const [name, summary] of groupSummaries) {
            const entries = [...controls].filter(([, item]) => item.groupName === name);
            const errors = entries.filter(([, item]) => !item.error.hidden).length;
            const filled = entries.filter(([key]) => document.fields[key]?.state === 'value').length;
            const cleared = entries.filter(([key]) => document.fields[key]?.state === 'cleared').length;
            summary.textContent = errors ? `${errors} 项需要修正` : [filled && `已填写 ${filled} 项`, cleared && `已清空 ${cleared} 项`].filter(Boolean).join(' · ');
            summary.hidden = !summary.textContent;
        }
    }
    function fieldControl(key, definition, group, groupName) {
        const readOnly = draft.isReadOnly() || definition.editable === false || definition.enabled === false || !schema.definitions[key];
        const wrapper = el('div', 'metadata-field');
        wrapper.setAttribute('data-field-key', key);
        const heading = el('div', 'metadata-field-heading');
        const id = `metadata-${key.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
        const label = el('label', '', definition.label || key);
        label.htmlFor = id;
        heading.appendChild(label);
        if (definition.export_status === 'internal_only') heading.appendChild(el('span', 'field-scope', '仅应用内保存'));
        wrapper.appendChild(heading);
        let control;
        if (definition.type === 'boolean' || definition.enum) {
            control = el('select');
            const options = definition.type === 'boolean' ? [['', '未填写'], ['true', '是'], ['false', '否']] : [['', '请选择'], ...definition.enum.map(value => [value, value === 'unknown' || value === 'Unknown' ? '未知' : value])];
            for (const [value, text] of options) {
                const option = el('option', '', text);
                option.value = value;
                control.appendChild(option);
            }
        } else if (['string[]', 'identifiers'].includes(definition.type) || key === 'summary') {
            control = el('textarea');
            control.rows = key === 'summary' ? 3 : 2;
        } else {
            control = el('input');
            control.type = definition.type === 'integer' ? 'number' : 'text';
            if (definition.type === 'integer') {
                control.step = '1';
                if (definition.minimum !== undefined) control.min = String(definition.minimum);
                if (definition.maximum !== undefined) control.max = String(definition.maximum);
            }
            if (definition.type === 'date') control.placeholder = '例如 2024、2024-03 或 2024-03-15';
        }
        control.id = id;
        control.name = `metadata.${key}`;
        control.disabled = readOnly;
        const hint = el('p', 'field-help');
        hint.id = `${id}-hint`;
        if (readOnly) hint.textContent = '此字段只读，保留已有值。';
        else if (definition.type === 'string[]') hint.textContent = '每行一项，名称中的空格会保留。';
        else if (definition.type === 'identifiers') hint.textContent = '每行填写类型:编号，例如 isbn:9780000000000。';
        else if (definition.type === 'date') hint.textContent = '只知道年份时，只填年份。';
        const error = el('p', 'field-error');
        error.id = `${id}-error`;
        error.hidden = true;
        control.setAttribute('aria-describedby', `${hint.id} ${error.id}`);
        wrapper.appendChild(control);
        wrapper.appendChild(hint);
        wrapper.appendChild(error);
        const footer = el('div', 'metadata-field-footer');
        const state = el('span', 'field-state');
        const clear = el('button', 'text-button', '清空');
        clear.type = 'button';
        clear.disabled = readOnly;
        clear.setAttribute('aria-label', `明确清空${definition.label || key}`);
        const unlock = el('button', 'text-button', '解除保护');
        unlock.type = 'button';
        footer.appendChild(state);
        footer.appendChild(clear);
        footer.appendChild(unlock);
        wrapper.appendChild(footer);
        group.appendChild(wrapper);
        const showError = errorValue => {
            draft.setInputValidity(key, !errorValue);
            error.textContent = errorValue || '';
            error.hidden = !errorValue;
            control.setAttribute('aria-invalid', errorValue ? 'true' : 'false');
            syncGroupSummaries(draft.getSnapshot());
        };
        listen(control, 'input', () => {
            pendingEdits = true;
            try { draft.setField(key, parseFieldInput(definition, control.value)); showError(''); }
            catch (failure) { showError(failure.message); }
        });
        listen(clear, 'click', () => {
            try { draft.clearField(key); control.value = ''; showError(''); control.focus(); }
            catch (failure) { showError(failure.message); }
        });
        listen(unlock, 'click', () => {
            try { draft.unlockField(key); showError(''); control.focus(); }
            catch (failure) { showError(failure.message); }
        });
        controls.set(key, { control, definition, state, unlock, readOnly, error, groupName });
    }
    function mount() {
        if (mounted || !root) return;
        mounted = true;
        root.replaceChildren();
        feedback = el('p', 'inline-feedback');
        feedback.setAttribute('role', 'status');
        feedback.setAttribute('aria-live', 'polite');
        root.appendChild(feedback);
        candidatePanel = el('section', 'metadata-candidates');
        candidatePanel.hidden = true;
        root.appendChild(candidatePanel);
        preparedReview = createCandidateReview({ draft, onChange: renderPrepared });
        const document = draft.getSnapshot();
        const definitions = { ...document.definition_snapshot, ...schema.definitions };
        // An older document must retain the definitions that gave its values meaning.
        if (draft.isReadOnly()) Object.assign(definitions, document.definition_snapshot);
        for (const name of groupOrder) {
            const entries = Object.entries(definitions).filter(([key, definition]) => groupFor(key) === name && (definition.enabled !== false || document.fields[key]));
            if (!entries.length) continue;
            entries.sort(([a], [b]) => (fieldOrder.indexOf(a) < 0 ? 99 : fieldOrder.indexOf(a)) - (fieldOrder.indexOf(b) < 0 ? 99 : fieldOrder.indexOf(b)));
            const details = el('details', 'metadata-group');
            details.open = name === '作品信息';
            const summary = el('summary', 'metadata-group-summary');
            summary.appendChild(el('span', 'metadata-group-name', name));
            const count = el('span', 'metadata-group-count');
            count.hidden = true;
            summary.appendChild(count);
            groupSummaries.set(name, count);
            details.appendChild(summary);
            const group = el('div', 'metadata-fields');
            details.appendChild(group);
            entries.forEach(([key, definition]) => fieldControl(key, definition, group, name));
            root.appendChild(details);
        }
        if (draft.isReadOnly()) feedback.textContent = '此草稿使用较早的字段定义，已保留原值。请核对定义后再编辑。';
        sync(document);
        unsubscribe = draft.subscribe(sync);
    }
    function cancelAdoption() {
        candidateGeneration += 1;
        adoptionController?.abort();
        adoptionController = null;
    }
    function showCandidate(candidate, { beforeApply } = {}) {
        cancelAdoption();
        candidateMode = candidate ? 'manual' : null;
        preparedReview?.clear();
        if (!candidate) {
            candidatePanel?.replaceChildren();
            if (candidatePanel) candidatePanel.hidden = true;
            return;
        }
        if (!mounted) return;
        const generation = candidateGeneration;
        candidatePanel.replaceChildren();
        candidatePanel.hidden = false;
        candidatePanel.appendChild(el('h3', '', '核对候选信息'));
        candidatePanel.appendChild(el('p', 'field-help', '选择要采用的字段。未选择的内容保持不变。'));
        for (const warning of candidate.warnings || []) candidatePanel.appendChild(el('p', 'field-help', warning.message || '请核对候选字段。'));
        let rows;
        try { rows = draft.previewCandidate(candidate); }
        catch (error) { feedback.textContent = error.message; return; }
        const selection = new Map();
        for (const row of rows) {
            const item = el('div', 'candidate-field');
            const label = el('label', 'candidate-selection');
            const checkbox = el('input');
            checkbox.type = 'checkbox';
            checkbox.disabled = Boolean(row.conflict);
            label.appendChild(checkbox);
            label.appendChild(el('span', '', row.definition.label));
            item.appendChild(label);
            const before = row.current?.state === 'cleared' ? '已明确清空' : row.current ? formatFieldValue(row.definition, row.current.value) : '未填写';
            const after = row.proposed.state === 'cleared' ? '明确清空' : formatFieldValue(row.definition, row.proposed.value);
            item.appendChild(el('p', 'candidate-value', `当前：${before}`));
            item.appendChild(el('p', 'candidate-value', `候选：${after}`));
            const sources = (row.proposed.provenance || []).map(source => sourceLabels[source.kind] || '已保存来源');
            item.appendChild(el('p', 'field-help', `来源：${[...new Set(sources)].join('、') || '未说明'}`));
            if (row.conflict || row.requiresConfirmation) item.appendChild(el('p', row.conflict ? 'field-error' : 'field-help', row.conflict || '此字段受手工保护，采用需要确认替换。'));
            candidatePanel.appendChild(item);
            selection.set(row.key, checkbox);
        }
        const confirm = el('input');
        confirm.type = 'checkbox';
        const confirmLabel = el('label', 'candidate-selection');
        confirmLabel.appendChild(confirm);
        confirmLabel.appendChild(el('span', '', '确认替换所选的手工保护字段'));
        candidatePanel.appendChild(confirmLabel);
        const apply = el('button', '', '采用所选字段');
        apply.type = 'button';
        const active = () => mounted && generation === candidateGeneration;
        const setBusy = busy => {
            apply.disabled = busy;
            confirm.disabled = busy;
            for (const row of rows) selection.get(row.key).disabled = busy || Boolean(row.conflict);
        };
        // Property listeners belong to the discarded panel, so replacing candidates does not accumulate listeners.
        apply.onclick = async () => {
            if (!active() || adoptionController) return;
            const keys = [...selection].filter(([, checkbox]) => checkbox.checked && !checkbox.disabled).map(([key]) => key);
            const confirmLocked = confirm.checked;
            if (!keys.length) { feedback.textContent = '请先选择要采用的字段。'; return; }
            if (beforeApply) {
                const request = new AbortController();
                adoptionController = request;
                setBusy(true);
                feedback.textContent = '正在核对当前来源设置与字段定义…';
                try {
                    if (await beforeApply({ signal: request.signal }) === false) throw new Error('rejected');
                } catch {
                    if (active() && !request.signal.aborted) feedback.textContent = '候选校验未通过，请重新获取候选或稍后重试。当前草稿已保留。';
                    request.abort();
                    return;
                } finally {
                    if (adoptionController === request) {
                        adoptionController = null;
                        if (active()) setBusy(false);
                    }
                }
                // A transport may ignore abort; replacement, cancellation and unmount still retire this panel.
                if (!active() || request.signal.aborted) return;
            }
            try {
                // Recheck field/input revisions after the asynchronous preflight as well.
                draft.applyCandidate(candidate, keys, { confirmLocked });
                preparedReview.recordAdopted({ candidate, beforeApply }, keys);
                showCandidate(null);
                feedback.textContent = '已采用所选字段，其他内容保持不变。';
            } catch (error) { feedback.textContent = error.message; }
        };
        candidatePanel.appendChild(apply);
        const cancel = el('button', 'text-button', '取消核对');
        cancel.type = 'button';
        cancel.onclick = () => {
            if (!active()) return;
            showCandidate(null);
            feedback.textContent = '已取消核对，当前草稿保持不变。';
        };
        candidatePanel.appendChild(cancel);
    }
    function renderPrepared(review) {
        if (!mounted || !candidatePanel || candidateMode !== 'prepared') return;
        candidatePanel.replaceChildren();
        candidatePanel.hidden = !review.pending.length && !review.prepared.length;
        if (review.pending.length) {
            candidatePanel.appendChild(el('h3', '', `${review.pending.length} 项信息需要核对`));
            candidatePanel.appendChild(el('p', 'field-help', '请选择有依据的值，也可保留当前内容或直接修改下方字段。'));
        }
        for (const group of review.pending) {
            const definition = draft.getDefinition(group.key);
            const section = el('section', 'candidate-field');
            section.setAttribute('data-review-field', group.key);
            section.appendChild(el('h4', '', definition.label || group.key));
            const current = group.current?.state === 'cleared' ? '已明确清空' : group.current ? formatFieldValue(definition, group.current.value) : '未填写';
            section.appendChild(el('p', 'candidate-value', `当前：${current}`));
            const controls = [];
            const decision = (text, selection) => {
                const button = el('button', 'btn btn-secondary', text); button.type = 'button';
                button.onclick = async () => {
                    controls.forEach(control => { control.disabled = true; });
                    try { await preparedReview.resolve(group.key, selection); }
                    catch (error) { if (mounted) feedback.textContent = error.message; }
                    finally { controls.forEach(control => { control.disabled = false; }); }
                };
                controls.push(button); section.appendChild(button);
            };
            group.options.forEach((option, index) => {
                const value = option.field.state === 'cleared' ? '明确清空' : formatFieldValue(definition, option.field.value);
                section.appendChild(el('p', 'candidate-value', `${option.sources.map(source => sourceLabels[source] || '已保存来源').join('、')}：${value}`));
                decision(`使用建议 ${index + 1}`, index);
            });
            decision(group.current ? '保留当前内容' : '暂不填写', 'keep');
            candidatePanel.appendChild(section);
        }
        if (review.prepared.length) {
            const details = el('details');
            details.appendChild(el('summary', '', `已准备 ${review.prepared.length} 项信息，查看来源`));
            for (const item of review.prepared) details.appendChild(el('p', 'field-help', `${draft.getDefinition(item.key)?.label || item.key}：${item.sources.map(source => sourceLabels[source] || '已保存来源').join('、')}`));
            candidatePanel.appendChild(details);
        }
    }
    return {
        mount,
        showCandidate,
        async prepareCandidates(entries, options) {
            if (!mounted) throw new Error('工作区尚未就绪。');
            cancelAdoption();
            candidateMode = 'prepared';
            renderPrepared(preparedReview.snapshot());
            return preparedReview.prepare(entries, options);
        },
        hasUnresolvedCandidates: () => Boolean(preparedReview?.hasUnresolved()),
        preflightPreparedCandidates: options => preparedReview?.preflight(options),
        hasPendingEdits: () => pendingEdits,
        markClean() { pendingEdits = false; },
        hasErrors: () => [...controls.values()].some(item => !item.error.hidden),
        unmount() {
            if (!mounted) return;
            mounted = false;
            candidateMode = null;
            cancelAdoption();
            preparedReview?.dispose();
            preparedReview = null;
            unsubscribe?.();
            unsubscribe = null;
            cleanup.splice(0).forEach(remove => remove());
            controls.clear();
            groupSummaries.clear();
            root.replaceChildren();
        },
    };
}
