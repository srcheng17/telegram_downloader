import { byteLength, decodeMetadataSchema, formatFieldValue } from '../shared/metadata/schema.js';
import { createOCRQueue, OCR_LANGUAGES } from './queue.js';
import { createLocalRecognizer } from './recognizer.js';
import { extractRules, validateRuleSet } from './rules.js';

const languageLabels = { chi_sim: '简体中文', chi_tra: '繁体中文', jpn: '日语', eng: '英语' };
const stateLabels = { queued: '等待识别', running: '正在识别', done: '识别完成', failed: '识别失败', cancelled: '已取消' };
const aiErrors = { disabled: 'AI 提取已停用。', not_configured: '请先保存 AI 服务和模型设置。', config_changed: 'AI 设置已变化，请重新载入后核对发送目标。', schema_unsupported: '模型尚不支持所选字段或已验证的完整上下文预算。', context_exceeded: '请求超出模型上下文，请手工减少文字或字段。', input_too_large: '发送文字过长，请手工减少。', refused: '模型拒绝本次提取。', unauthorized: 'AI 服务凭据无效。', forbidden: 'AI 服务拒绝访问。', rate_limited: 'AI 服务限流，请稍后手动重试。', timeout: 'AI 提取超时。', unreachable: '无法连接 AI 服务。', invalid_response: 'AI 结果缺少有效证据或不符合字段定义。', cancelled: 'AI 提取已取消。' };

// Owns only this slot. Root supplies the single draft/context coordinator and
// the CSRF-aware transport. All images/text remain in page memory.
export function mountOCR(root, { draft, api, schema: initialSchema, onCandidates = () => {}, onInputChange, setConfigRevision, recognizerFactory = createLocalRecognizer, queueOptions = {} } = {}) {
    const doc = root.ownerDocument || document; const schema = decodeMetadataSchema(initialSchema);
    let disposed = false; let localRevision = 0; let previousQueueRevision = 0;
    let ruleSet = null; let aiConfig = null; let aiController = null; let loadController = null;
    let previewRevision = null; let snapshotStale = true; let sendEdited = false; let merged = { text: '', segments: [], excluded: [], warnings: [] };
    const queue = createOCRQueue({ recognizerFactory, ...queueOptions }); const cards = new Map(); const cleanup = [];
    const node = (tag, text) => { const element = doc.createElement(tag); if (text !== undefined) element.textContent = text; return element; };
    const button = (text, action) => { const element = node('button', text); element.type = 'button'; element.className = 'btn btn-secondary'; element.onclick = action; return element; };
    const label = (parent, text, control) => { const element = node('label', text); element.appendChild(control); parent.appendChild(element); return control; };
    const listen = (element, name, handler) => { element.addEventListener(name, handler); cleanup.push(() => element.removeEventListener(name, handler)); };
    const status = node('p'); status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite');
    const feedback = text => { if (!disposed) status.textContent = text; };
    const candidateList = node('div'); candidateList.className = 'ocr-candidates';
    function context() { return draft.getContext(); }
    function cancelAI() { aiController?.abort(); aiController = null; send.disabled = !aiConfig?.enabled; }
    function change({ preserveSnapshot = false } = {}) {
        cancelAI(); localRevision++;
        if (onInputChange) onInputChange(); else draft.setContext({ ...context(), inputRevision: context().inputRevision + 1 });
        candidateList.replaceChildren(); onCandidates([]); confirmed.checked = false;
        if (!preserveSnapshot) { snapshotStale = true; previewState.textContent = sendEdited ? '来源已变化；已保留你编辑的发送文字。请核对后明确重新生成或保留此份。' : '来源已变化，请重新生成发送预览。'; }
    }
    function currentText() {
        const result = queue.merge({ acceptPartial: partial.checked });
        if (manual.value.trim()) { if (result.text) result.text += '\n\n'; result.text += manual.value; }
        return result;
    }
    function updateMerged() {
        try { merged = currentText(); mergedText.value = merged.text; mergeState.textContent = [...merged.warnings, ...(merged.excluded.length ? [`已明确排除 ${merged.excluded.length} 张未完成图片。`] : [])].join(' ') || '请先逐图校对识别文字。'; }
        catch (error) { merged = { text: '', segments: [], excluded: [], warnings: [] }; mergedText.value = ''; mergeState.textContent = error.message; }
    }
    function showResults(result, { rulesVersion, configVersion } = {}) {
        const revision = localRevision;
        candidateList.replaceChildren();
        for (const warning of result.warnings || []) candidateList.appendChild(node('p', warning.message));
        for (const candidate of result.candidates || []) {
            // The draft is the only adoption authority; this module does not merge.
            const text = Object.entries(candidate.fields).map(([key, field]) => `${schema.definitions[key]?.label || key}：${formatFieldValue(schema.definitions[key], field.value)}`).join('；');
            const beforeApply = async ({ signal }) => {
                const checkCurrent = () => {
                    if (disposed || signal.aborted || revision !== localRevision) throw new Error('候选已过期。');
                };
                checkCurrent();
                const [registry, settings] = await Promise.all([
                    api.getJson('/api/metadata/schema', { signal, cache: 'no-store' }),
                    api.getJson(candidate.origin === 'rule' ? '/api/settings/extraction-rules' : '/api/settings/ai', { signal, cache: 'no-store' }),
                ]);
                checkCurrent();
                if (!registry.response.ok || !settings.response.ok) throw new Error('候选校验暂不可用。');
                const currentSchema = decodeMetadataSchema(registry.payload);
                if (currentSchema.schema_version !== candidate.schema_version || currentSchema.definitions_version !== candidate.definitions_version) throw new Error('字段定义已变化。');
                if (candidate.origin === 'rule') {
                    if (validateRuleSet(settings.payload, currentSchema).rules_version !== rulesVersion) throw new Error('规则已变化。');
                } else if (settings.payload?.enabled !== true || !settings.payload.model_id || settings.payload.config_version !== configVersion) throw new Error('AI 设置已变化。');
            };
            const item = node('div'); item.appendChild(node('p', text)); item.appendChild(button('核对此候选', () => {
                if (disposed || revision !== localRevision) return;
                try { draft.previewCandidate(candidate); onCandidates([candidate], { beforeApply }); } catch (error) { feedback(error.message); }
            })); candidateList.appendChild(item);
        }
        feedback(result.candidates?.length ? `已生成 ${result.candidates.length} 项候选。点击核对后，在元数据面板选择采用。` : '未找到可核对的候选；当前草稿保持不变。');
    }
    function card(image) {
        const container = node('section'); container.className = 'ocr-image-card';
        const heading = node('h4'); const imageNode = node('img'); imageNode.src = image.previewURL; imageNode.alt = '待识别截图预览'; imageNode.loading = 'lazy'; imageNode.width = 160;
        const state = node('p'); const progress = node('progress'); progress.max = 1;
        const rawDetails = node('details'); rawDetails.appendChild(node('summary', '本次识别原文')); const raw = node('pre'); rawDetails.appendChild(raw);
        const edit = node('textarea'); edit.rows = 4; label(container, '校对文字（用于合并与提取）', edit);
        edit.oninput = () => { try { queue.edit(image.id, edit.value); } catch (error) { feedback(error.message); } };
        const retry = button('重新识别', () => queue.retry(image.id)); const cancel = button('取消此图', () => queue.cancel(image.id));
        container.appendChild(heading); container.appendChild(imageNode); container.appendChild(state); container.appendChild(progress); container.appendChild(rawDetails);
        for (const control of [button('上移', () => queue.move(image.id, -1)), button('下移', () => queue.move(image.id, 1)), retry, cancel, button('采用本次识别原文', () => queue.acceptRaw(image.id)), button('移除图片', () => queue.remove(image.id))]) container.appendChild(control);
        return { container, heading, state, progress, raw, edit, retry, cancel };
    }
    function renderQueue(snapshot) {
        if (disposed) return;
        if (snapshot.inputRevision !== previousQueueRevision) { previousQueueRevision = snapshot.inputRevision; change(); }
        const present = new Set(snapshot.images.map(image => image.id));
        for (const [id, elements] of cards) if (!present.has(id)) { elements.container.remove(); elements.edit.value = ''; elements.raw.textContent = ''; cards.delete(id); }
        snapshot.images.forEach((image, index) => {
            let elements = cards.get(image.id); if (!elements) { elements = card(image); cards.set(image.id, elements); }
            elements.heading.textContent = `图片 ${index + 1}`; elements.state.textContent = `${stateLabels[image.status]}${image.error ? '：' + image.error : ''}`; elements.progress.value = image.progress;
            elements.raw.textContent = image.rawText; if (elements.edit.value !== image.text) elements.edit.value = image.text;
            elements.retry.disabled = ['queued', 'running'].includes(image.status); elements.cancel.disabled = !['queued', 'running'].includes(image.status);
            imageList.appendChild(elements.container);
        }); updateMerged();
    }
    async function addImages(files) {
        const rejected = await queue.add(files);
        if (disposed) return;
        if (queue.snapshot().images.length) review.open = true;
        if (rejected.length) feedback(rejected.join(' '));
    }
    async function loadSettings() {
        // Even an unavailable or invalid replacement must retire old candidates.
        // The text and manually edited send snapshot remain in memory.
        change(); ruleSet = null; aiConfig = null; send.disabled = true;
        loadController?.abort(); const request = new AbortController(); loadController = request;
        try {
            const results = await Promise.allSettled([api.getJson('/api/settings/extraction-rules', { signal: request.signal }), api.getJson('/api/settings/ai', { signal: request.signal })]);
            if (disposed || request.signal.aborted || request !== loadController) return;
            const [rules, ai] = results;
            if (rules.status === 'fulfilled' && rules.value.response.ok) {
                try { const next = validateRuleSet(rules.value.payload, schema); if (ruleSet && ruleSet.rules_version !== next.rules_version) change(); ruleSet = next; rulesState.textContent = next.rules.length ? `已载入 ${next.rules.length} 条本地规则。` : '还没有提取规则，请到设置中添加。'; }
                catch (error) { ruleSet = null; rulesState.textContent = error.message; }
            } else { ruleSet = null; rulesState.textContent = '规则暂不可用；仍可手工录入。'; }
            if (ai.status === 'fulfilled' && ai.value.response.ok && Number.isSafeInteger(ai.value.payload?.config_version)) {
                const next = ai.value.payload;
                if (aiConfig?.config_version !== next.config_version) { change(); if (setConfigRevision) setConfigRevision(next.config_version); else draft.setContext({ ...context(), configRevision: next.config_version }); }
                aiConfig = next;
                destination.textContent = next.enabled && next.model_id ? `发送目标：${next.base_url}；模型：${next.model_id}` : 'AI 尚未启用或未选择模型；本地识别和规则仍然可用。';
            } else { aiConfig = null; destination.textContent = 'AI 设置暂不可用。'; }
            send.disabled = !aiConfig?.enabled || !aiConfig?.model_id;
        } catch { if (!disposed && !request.signal.aborted) feedback('设置载入失败，请重试。'); }
    }
    function runRules() {
        if (!ruleSet) { feedback('请先在设置中添加并保存规则。'); return; }
        try { const source = currentText(); showResults(extractRules({ text: source.text, segments: source.segments, ruleSet, schema, document: draft.getSnapshot(), ...context() }), { rulesVersion: ruleSet.rules_version }); }
        catch (error) { feedback(error.message); }
    }
    function refreshPreview(keep = false) {
        try {
            if (!keep) { sendText.value = currentText().text; sendEdited = false; }
            change({ preserveSnapshot: true }); snapshotStale = false; previewRevision = context().inputRevision;
            previewState.textContent = keep ? '已明确保留当前发送文字，请核对目标、字段并确认发送。' : '已生成发送副本；可在下方删改，确认后才会发送文字。';
        } catch (error) { feedback(error.message); }
    }
    async function extractAI() {
        if (!aiConfig?.enabled || !aiConfig?.model_id) { feedback('请先启用并保存 AI 设置。'); return; }
        if (snapshotStale || previewRevision !== context().inputRevision) { feedback('发送预览已过期，请重新生成或明确保留当前文字。'); return; }
        if (!confirmed.checked) { feedback('请核对发送文字、目标与字段，并勾选发送确认。'); return; }
        const keys = selections.filter(item => item.control.checked).map(item => item.key);
        if (!keys.length || !sendText.value.trim() || byteLength(sendText.value) > 65536) { feedback('请选择字段并填写不超过 64 KiB 的发送文字。'); return; }
        cancelAI(); const request = new AbortController(); aiController = request; const baseline = draft.getSnapshot(); const before = context(); const revision = localRevision; const requestID = `ai-${crypto.randomUUID()}`;
        const fieldRevisions = Object.fromEntries(keys.map(key => [key, baseline.fields[key]?.revision || 0]));
        const input = { request_id: requestID, text: sendText.value, field_keys: keys, schema_version: schema.schema_version, definitions_version: schema.definitions_version, base_document_revision: baseline.revision, field_revisions: fieldRevisions, input_revision: before.inputRevision, config_revision: aiConfig.config_version, ...(ruleSet ? { rules_version: ruleSet.rules_version } : {}) };
        send.disabled = true; feedback('正在等待 AI 提取，可取消；不会自动重试。');
        try {
            const { response, payload } = await api.postJson('/api/metadata/extract', input, { signal: request.signal });
            if (disposed || request.signal.aborted || aiController !== request) return;
            const current = context(); const document = draft.getSnapshot();
            if (revision !== localRevision || current.inputRevision !== before.inputRevision || current.configRevision !== before.configRevision || document.definitions_version !== baseline.definitions_version || keys.some(key => (document.fields[key]?.revision || 0) !== fieldRevisions[key])) { feedback('内容或字段已变化，旧的 AI 返回结果已丢弃。'); return; }
            if (!response.ok) { feedback((aiErrors[payload?.code] || 'AI 提取失败。') + ' 当前文字与草稿均已保留。'); return; }
            if (payload?.request_id !== requestID || !Array.isArray(payload.candidates) || payload.candidates.length > 64) throw new Error('invalid result');
            for (const candidate of payload.candidates) {
                if (candidate.request_id !== requestID || candidate.origin !== 'ai' || candidate.input_revision !== before.inputRevision || candidate.config_revision !== before.configRevision || Object.keys(candidate.fields).some(key => !keys.includes(key))) throw new Error('invalid candidate');
                draft.previewCandidate(candidate);
            }
            showResults(payload, { configVersion: input.config_revision });
        } catch { if (!disposed && !request.signal.aborted) feedback('AI 提取暂不可用或返回格式无效；当前文字与草稿已保留。'); }
        finally { if (aiController === request) { aiController = null; send.disabled = !aiConfig?.enabled; } }
    }

    root.replaceChildren(); root.appendChild(node('h3', '截图识别与提取'));
    root.appendChild(node('p', '同一作品的多张截图会合并识别。图片留在本机，AI 仅在确认后接收文字。'));
    const paste = node('div', '点击此处粘贴截图，或选择图片'); paste.tabIndex = 0; paste.className = 'ocr-paste-zone'; paste.setAttribute('role', 'region'); paste.setAttribute('aria-label', '专用截图粘贴区域'); root.appendChild(paste);
    const files = node('input'); files.type = 'file'; files.multiple = true; files.accept = 'image/png,image/jpeg,image/webp'; files.tabIndex = -1;
    const fileLabel = node('label', '选择截图（最多 10 张，每张 10 MiB，总计 50 MiB）'); fileLabel.className = 'sr-only'; fileLabel.appendChild(files); root.appendChild(fileLabel);
    paste.appendChild(button('选择截图', () => files.click()));
    paste.appendChild(node('small', 'PNG、JPG、WebP，最多 10 张'));
    listen(files, 'change', () => { void addImages(files.files); files.value = ''; });
    listen(paste, 'paste', event => { const images = [...(event.clipboardData?.items || [])].filter(item => item.kind === 'file' && item.type.startsWith('image/')).map(item => item.getAsFile()).filter(Boolean); if (!images.length) return; event.preventDefault(); void addImages(images); });
    const languageSet = node('fieldset'); languageSet.className = 'ocr-language-options'; languageSet.appendChild(node('legend', '识别语言'));
    const languages = OCR_LANGUAGES.map(key => { const control = node('input'); control.type = 'checkbox'; control.checked = ['chi_sim', 'eng'].includes(key); label(languageSet, languageLabels[key], control); return { key, control }; });
    languages.forEach(item => listen(item.control, 'change', () => { try { queue.setLanguages(languages.filter(value => value.control.checked).map(value => value.key)); } catch (error) { item.control.checked = true; feedback(error.message); } })); root.appendChild(languageSet);
    const imageList = node('div'); imageList.className = 'ocr-images'; root.appendChild(imageList);
    const rulesState = node('p'); root.appendChild(rulesState);
    const review = node('details'); review.className = 'ocr-edit-section'; review.appendChild(node('summary', '校对文字与提取')); root.appendChild(review);
    review.appendChild(button('取消待识别队列', () => queue.cancelAll()));
    const manual = node('textarea'); manual.rows = 3; label(review, '补充或手工录入文字（加入合并预览）', manual); listen(manual, 'input', () => { change(); updateMerged(); });
    const partial = node('input'); partial.type = 'checkbox'; label(review, '仅使用已完成图片（明确排除未完成或失败图片）', partial); listen(partial, 'change', () => { change(); updateMerged(); });
    const mergedText = node('textarea'); mergedText.readOnly = true; mergedText.rows = 4; label(review, '合并文字预览（保留图片顺序和段落）', mergedText); const mergeState = node('p'); review.appendChild(mergeState);
    review.appendChild(button('使用本地规则提取', runRules)); review.appendChild(button('重新载入规则与 AI 设置', loadSettings));
    const aiSection = node('details'); aiSection.appendChild(node('summary', '可选 AI 文字提取')); const destination = node('p', '正在读取已保存的 AI 设置…'); aiSection.appendChild(destination);
    aiSection.appendChild(button('从合并文字生成发送预览', () => refreshPreview())); aiSection.appendChild(button('明确保留当前发送文字', () => refreshPreview(true)));
    const sendText = node('textarea'); sendText.rows = 7; label(aiSection, '本次发送文字（可删改，只发送这一份）', sendText); const previewState = node('p'); aiSection.appendChild(previewState);
    listen(sendText, 'input', () => { sendEdited = true; change({ preserveSnapshot: true }); snapshotStale = false; previewRevision = context().inputRevision; previewState.textContent = '发送文字已编辑；请核对目标、字段并重新确认。'; });
    const fieldset = node('fieldset'); fieldset.appendChild(node('legend', '本次提取的字段'));
    const selections = Object.values(schema.definitions).filter(definition => definition.enabled && definition.extractable.includes('ai')).map(definition => { const control = node('input'); control.type = 'checkbox'; control.checked = ['title', 'creators.writer', 'summary', 'tags'].includes(definition.key); label(fieldset, definition.label, control); listen(control, 'change', () => { change({ preserveSnapshot: true }); previewRevision = context().inputRevision; }); return { key: definition.key, control }; }); aiSection.appendChild(fieldset);
    const confirmed = node('input'); confirmed.type = 'checkbox'; label(aiSection, '我已核对文字、服务目标、模型和字段，同意发送这份文字', confirmed);
    const send = button('发送文字并提取', extractAI); send.disabled = true; aiSection.appendChild(send); aiSection.appendChild(button('取消 AI 请求', () => { cancelAI(); feedback('已取消 AI 请求；当前文字与草稿已保留。'); })); review.appendChild(aiSection);
    root.appendChild(candidateList); root.appendChild(status);
    const unsubscribe = queue.subscribe(renderQueue); updateMerged(); void loadSettings();
    for (const event of ['metadata-definitions-changed', 'extraction-rules-changed', 'ai-config-changed']) listen(doc, event, () => { change(); feedback('设置已变化，请重新载入设置后核对。'); });
    return {
        queue, reloadSettings: loadSettings,
        isDirty: () => queue.snapshot().images.length > 0 || Boolean(manual.value || sendText.value),
        dispose() { if (disposed) return; disposed = true; aiController?.abort(); loadController?.abort(); unsubscribe(); cleanup.splice(0).forEach(remove => remove()); void queue.dispose(); cards.forEach(elements => { elements.edit.value = ''; elements.raw.textContent = ''; }); cards.clear(); manual.value = ''; mergedText.value = ''; sendText.value = ''; merged = null; ruleSet = null; aiConfig = null; root.replaceChildren(); },
    };
}
