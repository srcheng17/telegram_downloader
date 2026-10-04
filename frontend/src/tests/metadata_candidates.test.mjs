import test from 'node:test';
import assert from 'node:assert/strict';
import { candidateFromHistory } from '../shared/metadata/candidates.js';
import { createMetadataDraft } from '../shared/metadata/draft.js';

const title = { key: 'title', label: '标题', type: 'string', enabled: true, editable: true, extractable: ['legacy'] };
const oldCustom = { ...title, key: 'custom.user.old', label: '旧字段' };
function fixture() {
    const entry = { metadata_document: { schema_version: 1, definitions_version: 'old', revision: 1,
        definition_snapshot: { title, [oldCustom.key]: oldCustom },
        fields: { title: { state: 'value', value: '历史标题', revision: 1 }, [oldCustom.key]: { state: 'value', value: '只读值', revision: 1 } },
    } };
    const draft = createMetadataDraft({ definitions: { title, [oldCustom.key]: { ...oldCustom, enabled: false } }, definitionsVersion: 'new' });
    return { entry, draft };
}

test('adding optional definitions preserves explicit adoption of compatible old history fields', () => {
    const { entry, draft } = fixture();
    const before = structuredClone(entry);
    const candidate = candidateFromHistory({ entry, draft, requestId: 'history-1' });
    assert.deepEqual(Object.keys(candidate.fields), ['title']);
    assert.equal(candidate.definitions_version, 'new');
    assert.equal(candidate.warnings.length, 2);
    assert.equal(Object.keys(draft.getSnapshot().fields).length, 0);
    draft.applyCandidate(candidate, ['title']);
    assert.equal(draft.getSnapshot().fields.title.value, '历史标题');
    assert.deepEqual(entry, before);
});

test('future history schema and incompatible field types cannot be reinterpreted', () => {
    const { entry, draft } = fixture();
    entry.metadata_document.schema_version = 2;
    assert.throws(() => candidateFromHistory({ entry, draft, requestId: 'future' }), /版本/);
    entry.metadata_document.schema_version = 1;
    entry.metadata_document.definition_snapshot.title.type = 'integer';
    assert.throws(() => candidateFromHistory({ entry, draft, requestId: 'changed' }), /兼容/);
});
