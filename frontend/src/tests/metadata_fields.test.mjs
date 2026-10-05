import test from 'node:test';
import assert from 'node:assert/strict';
import { createMetadataFieldsModule } from '../settings/metadata_fields.js';
import { createDocument, walk } from './metadata_dom_fixture.mjs';

const limits = { custom_fields: 64, string_bytes: 4096, list_items: 64, list_item_bytes: 1024 };
const existing = { key: 'custom.user.note', label: '备注', type: 'string', max_bytes: 120, editable: true, enabled: true, extractable: ['rule'], export_status: 'internal_only' };
const flush = () => new Promise(resolve => setImmediate(resolve));
function fixture(overrides = {}) {
    const doc = createDocument();
    const root = doc.createElement('div');
    doc.querySelector = () => root;
    const saves = [];
    const api = {
        getJson: async () => ({ response: { ok: true }, payload: { definitions_version: 'v1', definitions: [existing], limits } }),
        putJson: async (url, data) => { saves.push({ url, data }); return { response: { ok: true }, payload: { ...data, definitions_version: 'v2', limits } }; },
        ...overrides,
    };
    const module = createMetadataFieldsModule({ doc, api });
    return { root, module, saves, find: id => walk(root).find(node => node.id === id) };
}

test('settings saves expected version and preserves disabled fields and immutable type', async () => {
    const { module, root, find, saves } = fixture();
    module.mount(); await flush();
    assert.equal(find('metadata-field-0-name').disabled, true);
    assert.equal(find('metadata-field-0-type').disabled, true);
    find('metadata-field-0-enabled').checked = false;
    find('metadata-fields-save').emit('click'); await flush();
    assert.equal(saves[0].data.expected_definitions_version, 'v1');
    assert.equal(saves[0].data.definitions[0].enabled, false);
    assert.equal(saves[0].data.definitions[0].max_bytes, 120);
    assert.equal(saves[0].data.definitions[0].key, existing.key);
    assert.match(walk(root).find(node => node.id === 'metadata-fields-status').textContent, /已保存/);
});

test('new fields derive bounds from registry limits and never export custom XML', async () => {
    const { module, find, saves } = fixture();
    module.mount(); await flush();
    find('metadata-fields-add').emit('click');
    find('metadata-field-1-name').value = 'approved';
    find('metadata-field-1-label').value = '确认状态';
    find('metadata-field-1-type').value = 'boolean';
    find('metadata-fields-save').emit('click'); await flush();
    const saved = saves[0].data.definitions[1];
    assert.equal(saved.key, 'custom.user.approved');
    assert.equal(saved.type, 'boolean');
    assert.equal(saved.export_status, 'internal_only');
    assert.equal(Object.hasOwn(saved, 'export_mapping'), false);
    assert.equal(Object.hasOwn(saved, 'max_bytes'), false);
});

test('409 preserves entered values and does not advance the expected version', async () => {
    const { module, find } = fixture({ putJson: async () => ({ response: { ok: false, status: 409 }, payload: { code: 'conflict' } }) });
    module.mount(); await flush();
    find('metadata-field-0-label').value = '保留我编辑的备注';
    find('metadata-fields-save').emit('click'); await flush();
    assert.equal(find('metadata-field-0-label').value, '保留我编辑的备注');
    assert.match(find('metadata-fields-status').textContent, /其他页面/);
});

test('unmount aborts owned requests, repeated mount is idempotent and late replies cannot render', async () => {
    let resolve;
    let signal;
    const { module, root } = fixture({ getJson: async (_url, options) => { signal = options.signal; return new Promise(done => { resolve = done; }); } });
    module.mount(); module.mount();
    assert.equal(walk(root).filter(node => node.id === 'metadata-fields-save').length, 1);
    module.unmount();
    assert.equal(signal.aborted, true);
    resolve({ response: { ok: true }, payload: { definitions_version: 'late', definitions: [existing], limits } });
    await flush();
    assert.equal(root.children.length, 0);
});

test('64-field cap and invalid new key prevent an invalid PUT', async () => {
    const maxDefs = Array.from({ length: 64 }, (_, i) => ({ ...existing, key: `custom.user.item_${i}` }));
    const { module, find } = fixture({ getJson: async () => ({ response: { ok: true }, payload: { definitions_version: 'v1', definitions: maxDefs, limits } }) });
    module.mount(); await flush();
    assert.equal(find('metadata-fields-add').disabled, true);
    module.unmount();
    const another = fixture(); another.module.mount(); await flush();
    another.find('metadata-fields-add').emit('click');
    another.find('metadata-field-1-name').value = 'Invalid key';
    another.find('metadata-field-1-label').value = '新字段';
    another.find('metadata-fields-save').emit('click'); await flush();
    assert.equal(another.saves.length, 0);
    assert.match(another.find('metadata-fields-status').textContent, /标识/);
});
