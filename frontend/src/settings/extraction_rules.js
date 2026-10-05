import { decodeMetadataSchema } from '../shared/metadata/schema.js';
import { extractRules, RULE_MODES, RULE_SEPARATORS, validateRuleSet } from '../ocr/rules.js';

const endpoint = '/api/settings/extraction-rules';

export function createExtractionRulesModule({ doc = document, api } = {}) {
    let root = null; let schema = null; let version = 0; let rows = []; let list; let status; let preview; let previewResult;
    let controller = null; let generation = 0; let busy = false; let save; let add; let reload;
    const element = (tag, text) => { const node = doc.createElement(tag); if (text !== undefined) node.textContent = text; return node; };
    const button = (text, action) => { const node = element('button', text); node.type = 'button'; node.className = 'btn btn-secondary'; node.onclick = action; return node; };
    const label = (parent, text, node) => { const wrapper = element('label', text); wrapper.appendChild(node); parent.appendChild(wrapper); return node; };
    const input = (type, value) => { const node = element('input'); node.type = type; if (type === 'checkbox') node.checked = value; else node.value = value; return node; };
    function controls() { if (!root) return; save.disabled = busy || !schema; add.disabled = busy || !schema || rows.length >= 128; reload.disabled = busy; rows.forEach(row => { row.container.disabled = busy; }); }
    function appendRow(rule = null) {
        const id = rule?.id || `rule-${crypto.randomUUID()}`;
        const container = element('fieldset'); container.className = 'settings-field-group'; container.appendChild(element('legend', '提取规则'));
        const target = element('select');
        for (const definition of Object.values(schema.definitions).filter(item => item.enabled && item.extractable.includes('rule'))) { const option = element('option', definition.label); option.value = definition.key; target.appendChild(option); }
        if (rule && !schema.definitions[rule.target_key]?.extractable?.includes('rule')) { const option = element('option', `${rule.target_key}（当前不可提取，请更换）`); option.value = rule.target_key; target.appendChild(option); }
        target.value = rule?.target_key || 'title'; label(container, '目标字段', target);
        const labels = element('textarea'); labels.value = rule?.labels.join('\n') || ''; labels.rows = 2; label(container, '识别标签（每行一个，最多 8 个）', labels);
        const mode = element('select'); for (const [value, text] of RULE_MODES) { const option = element('option', text); option.value = value; mode.appendChild(option); } mode.value = rule?.mode || 'label_value'; label(container, '提取方式', mode);
        const options = rule?.label_value_options || { separators: RULE_SEPARATORS.map(([key]) => key), case_insensitive: true, trim_space: true };
        const insensitive = label(container, '忽略英文标签大小写', input('checkbox', options.case_insensitive));
        const trim = label(container, '去除标签和值的首尾空白', input('checkbox', options.trim_space));
        const separators = RULE_SEPARATORS.map(([key, text]) => ({ key, control: label(container, `${text}分隔列表`, input('checkbox', options.separators.includes(key))) }));
        const row = { id, container, target, labels, mode, insensitive, trim, separators };
        container.appendChild(button('删除此规则', () => { rows = rows.filter(item => item !== row); container.remove(); controls(); }));
        rows.push(row); list.appendChild(container); controls();
    }
    function currentRules() {
        return rows.map(row => ({ id: row.id, target_key: row.target.value, labels: row.labels.value.split('\n').filter(value => value.trim()), mode: row.mode.value, label_value_options: { separators: row.separators.filter(item => item.control.checked).map(item => item.key), case_insensitive: row.insensitive.checked, trim_space: row.trim.checked } }));
    }
    function check(rules) { return validateRuleSet({ rules_version: version, definitions_version: schema.definitions_version, rules }, schema); }
    function previewRules() {
        if (!schema || busy) return;
        try {
            const result = extractRules({ text: preview.value, ruleSet: check(currentRules()), schema, document: { revision: 0, fields: {} }, inputRevision: 0, requestID: 'settings-preview' });
            previewResult.textContent = [...result.candidates.map(candidate => { const [key, field] = Object.entries(candidate.fields)[0]; return `${schema.definitions[key].label}：${JSON.stringify(field.value)}`; }), ...result.warnings.map(warning => warning.message)].join('\n') || '没有匹配结果。';
        } catch (error) { previewResult.textContent = error.message; }
    }
    async function load() {
        if (!root || busy) return; const local = root; const page = generation; const request = new AbortController(); controller = request; busy = true; controls(); status.textContent = '正在载入提取规则…';
        try {
            const [schemaResult, rulesResult] = await Promise.all([api.getJson('/api/metadata/schema', { signal: request.signal }), api.getJson(endpoint, { signal: request.signal })]);
            if (request.signal.aborted || root !== local || page !== generation) return;
            if (!schemaResult.response.ok || !rulesResult.response.ok) throw new Error('load failed');
            schema = decodeMetadataSchema(schemaResult.payload); const set = rulesResult.payload;
            if (!Number.isSafeInteger(set?.rules_version) || !Array.isArray(set.rules) || set.rules.length > 128) throw new Error('invalid rules');
            version = set.rules_version; rows = []; list.replaceChildren(); set.rules.forEach(appendRow);
            try { validateRuleSet(set, schema); status.textContent = '标签写在行首并以冒号分隔。规则仅在本机提取，预览文字不会保存。'; }
            catch (error) { status.textContent = error.message; }
        } catch { if (!request.signal.aborted && root === local && page === generation) status.textContent = '提取规则载入失败，请重试。'; }
        finally { if (controller === request) { busy = false; controller = null; controls(); } }
    }
    async function persist() {
        if (!root || busy || !schema) return; let rules;
        try { rules = currentRules(); check(rules); } catch (error) { status.textContent = error.message; return; }
        const local = root; const page = generation; const request = new AbortController(); controller = request; busy = true; controls();
        try {
            const { response, payload } = await api.putJson(endpoint, { expected_version: version, definitions_version: schema.definitions_version, rules }, { signal: request.signal });
            if (request.signal.aborted || root !== local || generation !== page) return;
            if (!response.ok) { status.textContent = response.status === 409 ? '规则已在其他页面更新；本页编辑保留，请核对后重新载入。' : '规则保存失败，请检查字段、标签与长度。'; return; }
            validateRuleSet(payload, schema); version = payload.rules_version; status.textContent = '提取规则已保存。';
            if (doc.defaultView?.CustomEvent) doc.dispatchEvent(new doc.defaultView.CustomEvent('extraction-rules-changed', { detail: { rulesVersion: version } }));
        } catch { if (!request.signal.aborted && root === local && generation === page) status.textContent = '规则保存失败，本页编辑已保留。'; }
        finally { if (controller === request) { controller = null; busy = false; controls(); } }
    }
    function unmount() { generation++; controller?.abort(); controller = null; busy = false; rows = []; schema = null; version = 0; root?.replaceChildren(); root = null; if (preview) preview.value = ''; if (previewResult) previewResult.textContent = ''; preview = null; previewResult = null; }
    function mount() {
        const next = doc.querySelector('[data-settings-slot="extraction-rules"]') || doc.querySelector('[data-module-slot="extraction-rules"]'); if (!next || next === root) return;
        unmount(); root = next; root.replaceChildren(); root.appendChild(element('h3', '本地提取规则'));
        list = element('div'); root.appendChild(list); add = button('新增规则', () => { if (schema && rows.length < 128) appendRow(); }); save = button('保存规则', persist); reload = button('重新载入规则', load); root.appendChild(add); root.appendChild(save); root.appendChild(reload);
        preview = element('textarea'); preview.rows = 5; label(root, '示例文字（仅用于本页预览）', preview); root.appendChild(button('预览当前规则', previewRules)); previewResult = element('pre'); root.appendChild(previewResult);
        status = element('p'); status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite'); root.appendChild(status); void load();
    }
    return { mount, unmount };
}
