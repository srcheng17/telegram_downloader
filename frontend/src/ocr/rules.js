import { byteLength, isRecord, parseFieldInput, validateFieldValue } from '../shared/metadata/schema.js';

export const RULE_MODES = [['label_value', '标签后的值'], ['continuation', '标签后的连续段落'], ['hashtag_list', '# 标签列表']];
export const RULE_SEPARATORS = [['comma', '逗号'], ['chinese_comma', '中文逗号'], ['semicolon', '分号'], ['newline', '换行']];
const characters = { comma: ',', chinese_comma: '，', semicolon: ';；', newline: '\n' };
const defaults = { separators: RULE_SEPARATORS.map(([key]) => key), case_insensitive: true, trim_space: true };
const invalid = () => { throw new Error('规则格式无效，请检查字段、标签、模式和分隔选项。'); };
const keysOnly = (value, allowed) => isRecord(value) && Object.keys(value).every(key => allowed.includes(key));
// Keep normalization identical to Go: Unicode White_Space, ASCII case only.
// ECMAScript's full Unicode lowercase/trim differ for dotted I, sigma and BOM.
const trimLabel = value => value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '');
const foldLabel = value => value.replace(/[A-Z]/g, letter => letter.toLowerCase());

export function validateRuleSet(ruleSet, schema) {
    if (!isRecord(ruleSet) || ruleSet.definitions_version !== schema.definitions_version) throw new Error('字段定义已变化，请在设置中核对并重新保存提取规则。');
    if (!Number.isSafeInteger(ruleSet.rules_version) || ruleSet.rules_version < 0 || !Array.isArray(ruleSet.rules) || ruleSet.rules.length > 128 || byteLength(JSON.stringify(ruleSet)) > 65536) invalid();
    const ids = new Set(); const labels = new Map();
    for (const rule of ruleSet.rules) {
        if (!keysOnly(rule, ['id', 'target_key', 'labels', 'mode', 'label_value_options']) || !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/u.test(rule.id) || ids.has(rule.id)) invalid();
        ids.add(rule.id); const definition = schema.definitions[rule.target_key];
        if (!definition?.enabled || !definition.extractable?.includes('rule') || !RULE_MODES.some(([mode]) => mode === rule.mode) || (rule.mode === 'continuation' && definition.type !== 'string') || (rule.mode === 'hashtag_list' && definition.type !== 'string[]')) invalid();
        if (!Array.isArray(rule.labels) || rule.labels.length < 1 || rule.labels.length > 8) invalid();
        const ownLabels = new Set();
        for (const label of rule.labels) {
            if (typeof label !== 'string' || !trimLabel(label) || byteLength(label) > 128 || /[\p{Cc}:：]/u.test(label)) invalid();
            const normalized = foldLabel(trimLabel(label));
            if (ownLabels.has(normalized) || (labels.has(normalized) && labels.get(normalized) !== rule.target_key)) invalid();
            ownLabels.add(normalized); labels.set(normalized, rule.target_key);
        }
        if (rule.label_value_options !== undefined) {
            const options = rule.label_value_options;
            if (!keysOnly(options, ['separators', 'case_insensitive', 'trim_space']) || typeof options.case_insensitive !== 'boolean' || typeof options.trim_space !== 'boolean' || !Array.isArray(options.separators) || new Set(options.separators).size !== options.separators.length || options.separators.some(key => !Object.hasOwn(characters, key))) invalid();
        }
    }
    return ruleSet;
}
function splitValue(raw, options) {
    let items = [raw];
    for (const key of options.separators) for (const character of characters[key]) items = items.flatMap(item => item.split(character));
    return items.map(item => options.trim_space ? item.trim() : item).filter(item => item.trim());
}
function parseValue(raw, rule, definition) {
    const options = rule.label_value_options || defaults;
    const text = options.trim_space ? raw.trim() : raw;
    if (!text.trim() || ['未知', '不详', 'unknown'].includes(text.trim().toLowerCase())) return undefined;
    let value;
    if (rule.mode === 'hashtag_list') value = [...text.matchAll(/#([^\s#]+)/gu)].map(match => match[1]);
    else if (definition.type === 'string[]') value = splitValue(text, options);
    else if (definition.type === 'identifiers') value = parseFieldInput(definition, splitValue(text, options).join('\n'));
    else if (definition.type === 'boolean') {
        const choices = { true: true, false: false, '是': true, '否': false };
        if (!Object.hasOwn(choices, text.toLowerCase())) throw new Error('是／否值无效。'); value = choices[text.toLowerCase()];
    } else value = parseFieldInput(definition, text);
    if (Array.isArray(value) && !value.length) return undefined;
    return validateFieldValue(definition, value);
}

export function extractRules({ text, ruleSet, schema, document, inputRevision, configRevision, requestID = `rules-${crypto.randomUUID()}`, segments = [], boundaryOffsets = new Set() }) {
    validateRuleSet(ruleSet, schema);
    if (typeof text !== 'string' || byteLength(text) > 65536) throw new Error('规则预览文字不能超过 64 KiB。');
    const lines = []; let offset = 0;
    for (const line of text.split('\n')) { lines.push({ text: line, start: offset, end: offset + line.length }); offset += line.length + 1; }
    function matches(line, rule) {
        const colon = line.search(/[:：]/u); if (colon < 0) return null;
        const options = rule.label_value_options || defaults;
        const normalize = value => { const next = options.trim_space ? trimLabel(value) : value; return options.case_insensitive ? foldLabel(next) : next; };
        return rule.labels.some(label => normalize(label) === normalize(line.slice(0, colon))) ? line.slice(colon + 1) : null;
    }
    const candidates = []; const warnings = []; const values = new Map();
    for (let index = 0; index < lines.length; index++) for (const rule of ruleSet.rules) {
        let raw = matches(lines[index].text, rule); if (raw === null) continue;
        let end = lines[index].end;
        if (rule.mode === 'continuation') {
            for (let cursor = index + 1; cursor < lines.length; cursor++) {
                // Unknown label-shaped lines are boundaries too: never absorb an
                // unconfigured role/value into a previous summary paragraph.
                const boundaryText = lines[cursor].text.replace(/\b\d{1,2}:\d{2}\b/gu, '');
                if (boundaryOffsets.has(lines[cursor].start) || /^\s*[^:：\n]{1,128}[:：]/u.test(boundaryText)) break;
                raw += '\n' + lines[cursor].text; end = lines[cursor].end;
            }
        }
        const key = rule.target_key; let value;
        try { value = parseValue(raw, rule, schema.definitions[key]); }
        catch { warnings.push({ key, code: 'invalid_value', message: '匹配到的文字不符合字段类型，已跳过，请手工核对。' }); continue; }
        if (value === undefined) continue;
        const signature = JSON.stringify(value); const previous = values.get(key) || new Set();
        if (previous.has(signature)) continue;
        if (previous.size && !warnings.some(item => item.key === key && item.code === 'conflicting_values')) warnings.push({ key, code: 'conflicting_values', message: '同一字段匹配到不同值，请分别对照后选择。' });
        previous.add(signature); values.set(key, previous);
        if (candidates.length >= 64) throw new Error('匹配到的建议过多，请缩小文字范围后重试。');
        const start = lines[index].start;
        const segment = segments.find(item => start >= item.start && end <= item.end);
        const evidence = segment ? { image_id: segment.image_id, start: start - segment.start, end: end - segment.start } : { start, end };
        candidates.push({ candidate_id: `${requestID}-${candidates.length}`, request_id: requestID, origin: 'rule', schema_version: schema.schema_version, definitions_version: schema.definitions_version, base_document_revision: document.revision, input_revision: inputRevision, ...(configRevision === undefined ? {} : { config_revision: configRevision }), field_revisions: { [key]: document.fields[key]?.revision || 0 }, fields: { [key]: { state: 'value', value, provenance: [{ kind: 'rule', source_id: 'local-rules', record_id: `rules:${ruleSet.rules_version}:text:${inputRevision}${segment ? `:image-text:${segment.text_revision}` : ''}`, evidence }] } } });
    }
    return { candidates, warnings };
}
