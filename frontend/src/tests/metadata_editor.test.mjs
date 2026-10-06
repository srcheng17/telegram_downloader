import test from 'node:test';
import assert from 'node:assert/strict';
import { createMetadataEditor } from '../shared/metadata/editor.js';
import { createMetadataDraft } from '../shared/metadata/draft.js';
import { createDocument, walk } from './metadata_dom_fixture.mjs';

function fixture() {
    const doc = createDocument();
    const root = doc.createElement('div');
    const definitions = {
        title: { key: 'title', label: '标题', type: 'string', max_bytes: 12, enabled: true, editable: true, extractable: ['provider'] },
        aliases: { key: 'aliases', label: '别名', type: 'string[]', max_items: 5, export_status: 'internal_only', enabled: true, editable: true },
        'custom.user.approved': { key: 'custom.user.approved', label: '已核对', type: 'boolean', export_status: 'internal_only', enabled: true, editable: true },
    };
    const schema = { definitions, definitions_version: 'v1' };
    const draft = createMetadataDraft({ definitions, definitionsVersion: 'v1' });
    const editor = createMetadataEditor({ root, draft, schema, doc });
    return { doc, root, draft, editor };
}

test('primary fields stay open while description and classification show a compact disclosure state', () => {
    const doc = createDocument();
    const root = doc.createElement('div');
    const keys = ['title', 'creators.writer', 'series', 'number', 'summary', 'tags', 'genres', 'aliases'];
    const definitions = Object.fromEntries(keys.map(key => [key, {
        key, label: key, type: ['creators.writer', 'tags', 'genres', 'aliases'].includes(key) ? 'string[]' : 'string',
        enabled: true, editable: true, max_bytes: 12,
    }]));
    const schema = { definitions, definitions_version: 'v1' };
    const draft = createMetadataDraft({ definitions, definitionsVersion: 'v1' });
    const editor = createMetadataEditor({ root, draft, schema, doc });
    editor.mount();
    const groups = root.children.filter(node => node.tagName === 'DETAILS');
    assert.deepEqual(groups.map(group => group.children[0].children[0].textContent), ['作品信息', '简介与分类']);
    assert.equal(groups[0].open, true);
    assert.equal(groups[1].open, false);
    assert.deepEqual(groups[0].children[1].children.map(field => field.getAttribute('data-field-key')), keys.slice(0, 4));
    assert.deepEqual(groups[1].children[1].children.map(field => field.getAttribute('data-field-key')), keys.slice(4));
    assert.ok(walk(root).filter(node => node.className === 'field-state').every(node => node.hidden));

    const summary = root.querySelector('#metadata-summary');
    summary.value = '简介'; summary.emit('input');
    assert.equal(groups[1].children[0].children[1].textContent, '已填写 1 项');
    assert.equal(groups[1].children[0].children[1].hidden, false);
    const clearSummary = walk(root).find(node => node.getAttribute('aria-label') === '明确清空summary');
    clearSummary.emit('click');
    assert.equal(groups[1].children[0].children[1].textContent, '已清空 1 项');
    const invalidTitle = root.querySelector('#metadata-title');
    invalidTitle.value = '中文中文中文中文'; invalidTitle.emit('input');
    assert.equal(groups[0].children[0].children[1].textContent, '1 项需要修正');
    invalidTitle.value = '短名'; invalidTitle.emit('input');
    assert.equal(groups[0].children[0].children[1].textContent, '已填写 1 项');
    editor.unmount();
});

test('editor creates registered extra/custom controls and does not move focus while typing', () => {
    const { doc, root, draft, editor } = fixture();
    editor.mount();
    const aliases = root.querySelector('#metadata-aliases');
    assert.ok(aliases);
    assert.ok(root.querySelector('#metadata-custom-user-approved'));
    const input = root.querySelector('#metadata-title');
    input.focus();
    input.value = '新标题';
    input.emit('input');
    assert.equal(doc.activeElement, input);
    assert.equal(draft.getSnapshot().fields.title.value, '新标题');
    assert.equal(input.value, '新标题');
    aliases.value = '<img src=x onerror=alert(1)>\n别名';
    aliases.emit('input');
    assert.equal(walk(root).filter(node => node.tagName === 'IMG').length, 0);
    assert.equal(draft.getSnapshot().fields.aliases.value.length, 2);
});

test('invalid typed input is visible and blocks submission while the prior document remains intact', () => {
    const { root, draft, editor } = fixture();
    editor.mount();
    const input = root.querySelector('#metadata-title');
    input.value = '短名'; input.emit('input');
    input.value = '中文中文中文中文'; input.emit('input');
    assert.equal(editor.hasErrors(), true);
    assert.equal(input.getAttribute('aria-invalid'), 'true');
    assert.equal(draft.getSnapshot().fields.title.value, '短名');
    const clear = walk(root).find(node => node.getAttribute('aria-label') === '明确清空标题');
    clear.emit('click');
    assert.equal(editor.hasErrors(), false);
    assert.equal(draft.getSnapshot().fields.title.state, 'cleared');
});

test('repeated mount does not duplicate controls, and unmount removes listeners', () => {
    const { root, draft, editor } = fixture();
    editor.mount(); editor.mount();
    const inputs = walk(root).filter(node => node.id === 'metadata-title');
    assert.equal(inputs.length, 1);
    editor.unmount();
    inputs[0].value = '迟到'; inputs[0].emit('input');
    assert.equal(draft.getSnapshot().revision, 0);
    assert.equal(root.children.length, 0);
});

test('invalid unsaved input also protects its field from an otherwise matching candidate', () => {
    const { root, draft, editor } = fixture();
    editor.mount();
    const candidate = { candidate_id: 'c1', request_id: 'r1', origin: 'provider', schema_version: 1, definitions_version: 'v1', base_document_revision: 0, input_revision: 0, field_revisions: { title: 0 }, fields: { title: { state: 'value', value: '候选', provenance: [{ kind: 'provider', source_id: 'test' }] } } };
    const input = root.querySelector('#metadata-title');
    input.value = '中文中文中文中文'; input.emit('input');
    assert.equal(draft.isDirty(), true);
    assert.throws(() => draft.applyCandidate(candidate, ['title']), /输入/);
    assert.equal(draft.getSnapshot().fields.title, undefined);
});

const candidate = () => ({ candidate_id: 'c1', request_id: 'r1', origin: 'provider', schema_version: 1, definitions_version: 'v1', base_document_revision: 0, input_revision: 0, field_revisions: { title: 0 }, fields: { title: { state: 'value', value: '候选', provenance: [{ kind: 'provider', source_id: 'test' }] } } });
const nextTick = () => new Promise(resolve => setImmediate(resolve));
function selectCandidate(f, options) {
    f.editor.showCandidate(candidate(), options);
    const panel = walk(f.root).find(node => node.className === 'metadata-candidates');
    const checkbox = walk(panel).find(node => node.type === 'checkbox');
    checkbox.checked = true;
    const apply = walk(panel).find(node => node.textContent === '采用所选字段');
    return { panel, checkbox, apply };
}

test('adoption awaits authoritative preflight, disables controls and ignores repeated clicks', async () => {
    const f = fixture(); f.editor.mount(); let done; let signal; let calls = 0;
    const { checkbox, apply } = selectCandidate(f, { beforeApply: options => { calls++; signal = options.signal; return new Promise(resolve => { done = resolve; }); } });
    apply.emit('click'); apply.emit('click');
    assert.equal(calls, 1); assert.equal(checkbox.disabled, true); assert.equal(apply.disabled, true);
    assert.equal(f.draft.getSnapshot().revision, 0); assert.equal(signal.aborted, false);
    done(); await nextTick();
    assert.equal(f.draft.getSnapshot().fields.title.value, '候选');
    f.editor.unmount();
});

test('preflight rejection preserves the draft, permits retry and does not expose backend text', async () => {
    const f = fixture(); f.editor.mount(); let signal;
    const { checkbox, apply } = selectCandidate(f, { beforeApply: async options => { signal = options.signal; throw new Error('private-upstream-body'); } });
    apply.emit('click'); await nextTick();
    assert.equal(f.draft.getSnapshot().revision, 0); assert.equal(checkbox.disabled, false); assert.equal(apply.disabled, false);
    assert.equal(signal.aborted, true, 'release any sibling authority request after one fails');
    const feedback = walk(f.root).find(node => node.className === 'inline-feedback').textContent;
    assert.match(feedback, /校验.*草稿.*保留/); assert.ok(!feedback.includes('private-upstream-body'));
    f.editor.unmount();
});

test('cancel, replacement, clearing and unmount abort pending adoption and discard late success', async () => {
    for (const action of ['cancel', 'replace', 'clear', 'unmount']) {
        const f = fixture(); f.editor.mount(); let done; let signal;
        const { apply } = selectCandidate(f, { beforeApply: options => { signal = options.signal; return new Promise(resolve => { done = resolve; }); } });
        apply.emit('click');
        if (action === 'cancel') walk(f.root).find(node => node.textContent === '取消核对').emit('click');
        if (action === 'replace') selectCandidate(f);
        if (action === 'clear') f.editor.showCandidate(null);
        if (action === 'unmount') f.editor.unmount();
        assert.equal(signal.aborted, true, action);
        done(); await nextTick(); apply.emit('click'); await nextTick();
        assert.equal(f.draft.getSnapshot().revision, 0, action);
        if (action === 'unmount') assert.equal(f.root.children.length, 0);
        f.editor.unmount();
    }
});

test('local field, invalid input and context changes while checking still block atomic adoption', async () => {
    for (const action of ['field', 'invalid', 'context']) {
        const f = fixture(); f.editor.mount(); let done;
        const { apply } = selectCandidate(f, { beforeApply: () => new Promise(resolve => { done = resolve; }) });
        apply.emit('click');
        if (action === 'field') f.draft.clearField('title');
        if (action === 'invalid') f.draft.setInputValidity('title', false);
        if (action === 'context') f.draft.setContext({ inputRevision: 2 });
        const before = f.draft.getSnapshot(); done(); await nextTick();
        assert.deepEqual(f.draft.getSnapshot(), before, action);
        f.editor.unmount();
    }
});

test('candidates without preflight retain explicit synchronous adoption', () => {
    const f = fixture(); f.editor.mount();
    selectCandidate(f).apply.emit('click');
    assert.equal(f.draft.getSnapshot().fields.title.value, '候选'); f.editor.unmount();
});

test('manual field changes keep an open historical candidate visible and reject its stale revision', () => {
    const f = fixture(); f.editor.mount();
    const { panel, checkbox, apply } = selectCandidate(f);
    const clear = walk(f.root).find(node => node.getAttribute('aria-label') === '明确清空标题');
    clear.emit('click');
    assert.equal(panel.hidden, false);
    assert.ok(walk(panel).includes(checkbox));
    apply.emit('click');
    assert.match(walk(f.root).find(node => node.className === 'inline-feedback').textContent, /过期/);
    assert.equal(f.draft.getSnapshot().fields.title.state, 'cleared');
    f.editor.unmount();
});

test('final confirmation rechecks advanced adopted authority until the field is manually changed', async () => {
    const f = fixture(); f.editor.mount();
    let current = true; let checks = 0;
    const beforeApply = async () => { checks++; if (!current) throw new Error('retired source'); };
    selectCandidate(f, { beforeApply }).apply.emit('click'); await nextTick();
    assert.equal(f.draft.getSnapshot().fields.title.value, '候选');
    assert.equal(checks, 1);
    await f.editor.preflightPreparedCandidates(); assert.equal(checks, 2);
    current = false;
    await assert.rejects(f.editor.preflightPreparedCandidates(), /设置已变化/);
    assert.equal(f.draft.getSnapshot().fields.title.value, '候选');
    f.draft.setField('title', '人工核对');
    await f.editor.preflightPreparedCandidates(); assert.equal(checks, 3);
    f.editor.unmount();
});

test('guided review prefills safe fields and resolves only actual differences in the same editor', async () => {
    const f = fixture(); f.editor.mount();
    await f.editor.prepareCandidates([{ candidate: candidate(), beforeApply: async () => {} }]);
    assert.equal(f.draft.getSnapshot().fields.title.value, '候选');
    assert.equal(f.editor.hasUnresolvedCandidates(), false);
    assert.ok(!walk(f.root).some(node => node.textContent === '采用所选字段'));
    const changed = candidate(); changed.base_document_revision = f.draft.getSnapshot().revision;
    changed.field_revisions.title = f.draft.getSnapshot().fields.title.revision;
    changed.fields.title.value = '另一项';
    await f.editor.prepareCandidates([{ candidate: changed, beforeApply: async () => {} }]);
    assert.equal(f.editor.hasUnresolvedCandidates(), true);
    walk(f.root).find(node => node.textContent === '保留当前内容').emit('click');
    await nextTick();
    assert.equal(f.editor.hasUnresolvedCandidates(), false);
    assert.equal(f.draft.getSnapshot().fields.title.value, '候选');
    f.editor.unmount();
});
