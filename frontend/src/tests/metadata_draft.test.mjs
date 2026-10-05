import test from 'node:test';
import assert from 'node:assert/strict';
import { createMetadataDraft } from '../shared/metadata/draft.js';
import { decodeMetadataSchema, validateFieldValue } from '../shared/metadata/schema.js';

export function schemaFixture() {
    const base = { editable: true, enabled: true, extractable: ['provider', 'ai'], export_status: 'mapped' };
    return decodeMetadataSchema({ schema_version: 1, definitions_version: 'test-v1', definitions: {
        title: { ...base, key: 'title', label: '标题', type: 'string', max_bytes: 12 },
        summary: { ...base, key: 'summary', label: '简介', type: 'string', max_bytes: 1024 },
        aliases: { ...base, key: 'aliases', label: '别名', type: 'string[]', max_items: 3, item_max_bytes: 32, export_status: 'internal_only' },
        'custom.user.checked': { ...base, key: 'custom.user.checked', label: '已核对', type: 'boolean', export_status: 'internal_only' },
        'custom.user.index': { ...base, key: 'custom.user.index', label: '索引', type: 'integer', minimum: 0, maximum: 20, export_status: 'internal_only' },
    }, limits: { document_bytes: 262144, provenance_per_field: 8 } });
}
function fixture() {
    const schema = schemaFixture();
    const draft = createMetadataDraft({ definitions: schema.definitions, definitionsVersion: schema.definitions_version, limits: schema.limits });
    draft.setContext({ inputRevision: 2, configRevision: 3 });
    return draft;
}
function suggestion(draft, fields) {
    const document = draft.getSnapshot();
    return { candidate_id: 'c1', request_id: 'r1', origin: 'provider', schema_version: 1, definitions_version: document.definitions_version,
        base_document_revision: document.revision, input_revision: 2, config_revision: 3,
        field_revisions: Object.fromEntries(Object.keys(fields).map(key => [key, document.fields[key]?.revision || 0])),
        fields: Object.fromEntries(Object.entries(fields).map(([key, value]) => [key, { state: 'value', value, provenance: [{ kind: 'provider', source_id: 'fixture' }] }])), warnings: [] };
}

test('typed metadata preserves false, zero and empty lists; explicit clear is a locked tombstone', () => {
    const draft = fixture();
    draft.setField('custom.user.checked', false);
    draft.setField('custom.user.index', 0);
    draft.setField('aliases', []);
    assert.equal(draft.getSnapshot().fields['custom.user.checked'].value, false);
    assert.equal(draft.getSnapshot().fields['custom.user.index'].value, 0);
    assert.deepEqual(draft.getSnapshot().fields.aliases.value, []);
    draft.clearField('title');
    assert.deepEqual(draft.getSnapshot().fields.title.state, 'cleared');
    assert.equal(draft.getSnapshot().fields.title.manual_locked, true);
    assert.equal(Object.hasOwn(draft.getSnapshot().fields.title, 'value'), false);
    assert.equal(Object.hasOwn(draft.getSnapshot().fields, 'summary'), false);
});

test('candidate selected before a manual clear cannot restore it or partially apply other fields', () => {
    const draft = fixture();
    const candidate = suggestion(draft, { title: '旧标题', summary: '候选简介' });
    draft.clearField('title');
    const before = draft.getSnapshot();
    assert.throws(() => draft.applyCandidate(candidate, ['title', 'summary'], { confirmLocked: true }), /已变化|过期/);
    assert.deepEqual(draft.getSnapshot(), before);
});

test('unselected edits survive an atomic candidate adoption; locked values require explicit confirmation', () => {
    const draft = fixture();
    const candidate = suggestion(draft, { title: '候选' });
    draft.setField('summary', '手工简介');
    draft.applyCandidate(candidate, ['title']);
    assert.equal(draft.getSnapshot().fields.summary.value, '手工简介');
    assert.equal(draft.getSnapshot().revision, 2);
    draft.setField('title', '手工');
    const replacement = suggestion(draft, { title: '新值' });
    assert.throws(() => draft.applyCandidate(replacement, ['title']), /确认/);
    draft.applyCandidate(replacement, ['title'], { confirmLocked: true });
    assert.equal(draft.getSnapshot().fields.title.value, '新值');
    assert.equal(draft.getSnapshot().fields.title.manual_locked, true);
});

test('config/input changes invalidate suggestions, copies cannot mutate draft, disposal clears state', () => {
    const draft = fixture();
    const candidate = suggestion(draft, { title: '候选' });
    draft.setContext({ inputRevision: 3, configRevision: 3 });
    assert.throws(() => draft.applyCandidate(candidate, ['title']), /过期/);
    const copy = draft.getSnapshot();
    copy.fields.title = { value: '外部修改' };
    assert.equal(draft.getSnapshot().fields.title, undefined);
    draft.dispose();
    assert.throws(() => draft.setField('title', '晚到'), /销毁/);
});

test('types and UTF-8 byte limits reject rather than truncate', () => {
    const schema = schemaFixture();
    assert.throws(() => validateFieldValue(schema.definitions.title, '中文中文中'), /长度/);
    assert.throws(() => validateFieldValue(schema.definitions['custom.user.checked'], 'false'), /布尔/);
    assert.throws(() => validateFieldValue(schema.definitions['custom.user.index'], 1.5), /整数/);
    assert.throws(() => decodeMetadataSchema({ schema_version: 2, definitions_version: 'v2', definitions: {} }), /版本/);
});

test('Go-owned shared fixture produces the same candidate document and definition snapshots in JavaScript', async () => {
    const { readFile } = await import('node:fs/promises');
    const shared = JSON.parse(await readFile(new URL('../../../internal/domain/metadata/testdata/contract-v1.json', import.meta.url), 'utf8'));
    const schema = decodeMetadataSchema(shared.schema);
    const draft = createMetadataDraft({ document: shared.document, definitions: schema.definitions, definitionsVersion: schema.definitions_version, limits: schema.limits, now: () => '2026-10-04T00:00:00Z' });
    draft.setContext({ inputRevision: shared.patch.input_revision, configRevision: shared.patch.config_revision });
    const adopt = shared.patch.operations.find(operation => operation.op === 'adopt');
    assert.deepEqual(draft.applyCandidate(adopt.candidate, adopt.keys, { confirmLocked: adopt.confirm_locked }), shared.result.document);
});
