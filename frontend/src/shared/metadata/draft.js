import { byteLength, copy, isRecord, validateFieldValue } from './schema.js';

const origins = new Set(['archive', 'ocr', 'rule', 'ai', 'provider', 'legacy']);

export function createMetadataDraft({ document, definitions, definitionsVersion, limits = {}, now = () => new Date().toISOString() }) {
    let current = copy(document || { schema_version: 1, definitions_version: definitionsVersion, revision: 0, fields: {}, definition_snapshot: {} });
    if (!isRecord(definitions) || current.schema_version !== 1 || !Number.isSafeInteger(current.revision) || current.revision < 0 || !isRecord(current.fields) || !isRecord(current.definition_snapshot)) throw new Error('元数据草稿结构无效。');
    const registry = copy(definitions);
    const listeners = new Set();
    const invalidInputs = new Set();
    let disposed = false;
    let inputRevision = 0;
    let configRevision;
    let initialRevision = current.revision;
    const ensureLive = () => { if (disposed) throw new Error('当前草稿已销毁，请重新打开工作区。'); };
    const definitionFor = key => {
        ensureLive();
        if (current.definitions_version !== definitionsVersion) throw new Error('字段定义版本已变化，请核对后更新草稿。');
        const definition = Object.hasOwn(registry, key) ? registry[key] : null;
        if (!definition || definition.enabled === false || definition.editable === false) throw new Error('此字段只读或已停用。');
        return definition;
    };
    function publish(next) {
        if (byteLength(JSON.stringify(next)) > (limits.document_bytes || 262144)) throw new Error('元数据总长度超限，请减少内容。');
        current = next;
        listeners.forEach(listener => listener(copy(current)));
        return copy(current);
    }
    function mutate(key, operation, value) {
        const definition = definitionFor(key);
        const next = copy(current);
        if (operation === 'unlock' && !next.fields[key]) return copy(current);
        next.revision += 1;
        if (!Number.isSafeInteger(next.revision)) throw new Error('元数据修订超出范围。');
        next.definition_snapshot[key] = copy(definition);
        if (operation === 'unlock') next.fields[key] = { ...next.fields[key], manual_locked: false, revision: next.revision };
        else next.fields[key] = { state: operation === 'clear' ? 'cleared' : 'value', ...(operation === 'clear' ? {} : { value: validateFieldValue(definition, value) }), revision: next.revision, manual_locked: true, provenance: [{ kind: 'manual', source_id: 'user', adopted_at: now() }] };
        const result = publish(next);
        invalidInputs.delete(key);
        return result;
    }
    function checkCandidate(candidate, keys, confirmLocked) {
        ensureLive();
        if (!isRecord(candidate) || !origins.has(candidate.origin) || !candidate.candidate_id || !candidate.request_id || !isRecord(candidate.fields) || !isRecord(candidate.field_revisions)) throw new Error('候选格式无效。');
        if (candidate.schema_version !== current.schema_version || candidate.definitions_version !== current.definitions_version || current.definitions_version !== definitionsVersion || candidate.input_revision !== inputRevision || candidate.config_revision !== configRevision || !Number.isSafeInteger(candidate.base_document_revision) || candidate.base_document_revision > current.revision) throw new Error('候选已过期，请重新获取。');
        for (const key of keys) {
            const definition = definitionFor(key);
            if (invalidInputs.has(key)) throw new Error('此字段有尚未修正的输入，请先处理手工修改。');
            const field = candidate.fields[key];
            if (!Object.hasOwn(candidate.fields, key) || !isRecord(field) || Object.keys(field).some(name => !['state', 'value', 'provenance', 'warnings'].includes(name)) || !['value', 'cleared'].includes(field.state) || !Array.isArray(field.provenance) || field.provenance.length > (limits.provenance_per_field || 8)) throw new Error('候选字段无效。');
            if (field.provenance.some(source => !isRecord(source) || source.kind !== candidate.origin || typeof source.source_id !== 'string' || !source.source_id)) throw new Error('候选来源与字段来源不一致。');
            if (Array.isArray(definition.extractable) && !definition.extractable.includes(candidate.origin)) throw new Error('此来源不能填写选定字段。');
            if (!Object.hasOwn(candidate.field_revisions, key) || candidate.field_revisions[key] !== (current.fields[key]?.revision || 0)) throw new Error('字段已变化，候选已过期。');
            if (current.fields[key]?.manual_locked && !confirmLocked) throw new Error('此字段已手工修改，请明确确认替换。');
            if (field.state === 'value') validateFieldValue(definition, field.value);
            else if (Object.hasOwn(field, 'value')) throw new Error('清空候选不能携带字段值。');
        }
    }
    return {
        getDefinition(key) { ensureLive(); return copy(registry[key]); },
        getSnapshot() { ensureLive(); return copy(current); },
        markClean(revision = current.revision) { ensureLive(); initialRevision = revision; },
        isDirty() { return !disposed && (current.revision !== initialRevision || invalidInputs.size > 0); },
        setInputValidity(key, valid) { ensureLive(); if (valid) invalidInputs.delete(key); else invalidInputs.add(key); },
        isReadOnly() { return current.definitions_version !== definitionsVersion; },
        subscribe(listener) { ensureLive(); listeners.add(listener); return () => listeners.delete(listener); },
        setContext(context) { ensureLive(); inputRevision = context.inputRevision; configRevision = context.configRevision; },
        getContext() { return { inputRevision, configRevision }; },
        setField: (key, value) => mutate(key, 'set', value),
        clearField: key => mutate(key, 'clear'),
        unlockField: key => mutate(key, 'unlock'),
        previewCandidate(candidate) {
            ensureLive();
            if (!isRecord(candidate?.fields)) throw new Error('候选格式无效。');
            return Object.entries(candidate.fields).map(([key, proposed]) => {
                let conflict = '';
                try { checkCandidate(candidate, [key], true); } catch (error) { conflict = error.message; }
                return { key, definition: copy(registry[key] || current.definition_snapshot[key] || { key, label: key, type: 'string' }), current: copy(current.fields[key]), proposed: copy(proposed), conflict, requiresConfirmation: Boolean(current.fields[key]?.manual_locked) };
            });
        },
        applyCandidate(candidate, selectedKeys, { confirmLocked = false } = {}) {
            if (!Array.isArray(selectedKeys) || !selectedKeys.length) throw new Error('请先选择要采用的字段。');
            const keys = [...new Set(selectedKeys)];
            checkCandidate(candidate, keys, confirmLocked);
            const next = copy(current);
            next.revision += 1;
            for (const key of keys) {
                const field = candidate.fields[key];
                next.fields[key] = { state: field.state, ...(field.state === 'value' ? { value: copy(field.value) } : {}), revision: next.revision, manual_locked: Boolean(current.fields[key]?.manual_locked), provenance: field.provenance.map(source => ({ ...copy(source), adopted_at: now() })) };
                next.definition_snapshot[key] = copy(registry[key]);
            }
            return publish(next);
        },
        dispose() { disposed = true; current = null; listeners.clear(); invalidInputs.clear(); },
    };
}
