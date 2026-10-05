import { copy, validateFieldValue } from './schema.js';

export function readHistoryDocument(entry, view = 'submitted') {
    if (!['submitted', 'effective'].includes(view)) throw new Error('请选择提交信息或最终归档信息。');
    const document = view === 'effective' ? entry?.effective_metadata_document : entry?.metadata_document;
    if (!document) return null;
    return copy(document);
}

export function candidateFromHistory({ entry, view = 'submitted', draft, requestId }) {
    const source = readHistoryDocument(entry, view);
    if (!source) throw new Error(view === 'effective' ? '此记录暂无最终归档元数据。' : '此记录暂无可读取的提交元数据。');
    const document = draft.getSnapshot();
    if (source.schema_version !== document.schema_version) throw new Error('历史文档版本尚不支持兼容转换。');
    const needsConversion = source.definitions_version !== document.definitions_version;
    const warnings = [];
    const fields = {};
    for (const [key, field] of Object.entries(source.fields)) {
        if (needsConversion) {
            const previous = source.definition_snapshot?.[key];
            const current = draft.getDefinition?.(key);
            if (!previous || !current || previous.type !== current.type || current.enabled === false || current.editable === false || !current.extractable?.includes('legacy')) {
                warnings.push({ key, code: 'incompatible_history_field', message: `${previous?.label || key} 与当前字段不兼容，保留只读预览。` });
                continue;
            }
            try { if (field.state === 'value') validateFieldValue(current, field.value); }
            catch { warnings.push({ key, code: 'invalid_history_field', message: `${previous.label || key} 不符合当前字段限制，保留只读预览。` }); continue; }
        }
        fields[key] = { state: field.state, ...(field.state === 'value' ? { value: copy(field.value) } : {}), provenance: [{ kind: 'legacy', source_id: 'history' }] };
    }
    if (needsConversion && !Object.keys(fields).length) throw new Error('此历史没有可兼容采用的字段，请参考只读预览。');
    if (needsConversion) warnings.unshift({ code: 'history_definitions_converted', message: '历史使用较早的字段定义。已检查可兼容的字段，请逐项确认后采用。' });
    const context = draft.getContext();
    return { candidate_id: `history-${requestId}`, request_id: requestId, origin: 'legacy', schema_version: document.schema_version, definitions_version: document.definitions_version, base_document_revision: document.revision,
        input_revision: context.inputRevision, ...(context.configRevision === undefined ? {} : { config_revision: context.configRevision }),
        field_revisions: Object.fromEntries(Object.keys(fields).map(key => [key, document.fields[key]?.revision || 0])), fields, warnings };
}
