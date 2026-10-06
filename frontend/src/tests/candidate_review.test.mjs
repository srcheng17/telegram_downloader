import test from 'node:test';
import assert from 'node:assert/strict';
import { createMetadataDraft } from '../shared/metadata/draft.js';
import { createCandidateReview } from '../shared/metadata/candidate_review.js';

function fixture() {
    const definitions = Object.fromEntries(['title', 'summary'].map(key => [key, { key, label: key, type: 'string', enabled: true, editable: true, max_bytes: 1024, extractable: ['ocr', 'ai', 'provider'] }]));
    const draft = createMetadataDraft({ definitions, definitionsVersion: 'v1' });
    const review = createCandidateReview({ draft });
    function entry(value, { key = 'title', origin = 'ocr', beforeApply = async () => {}, warnings } = {}) {
        const doc = draft.getSnapshot(), context = draft.getContext();
        return { beforeApply, candidate: { candidate_id: `${origin}-${key}-${value}`, request_id: 'r1', origin, schema_version: 1, definitions_version: 'v1', base_document_revision: doc.revision, input_revision: context.inputRevision, ...(context.configRevision === undefined ? {} : { config_revision: context.configRevision }), field_revisions: { [key]: doc.fields[key]?.revision || 0 }, fields: { [key]: { state: 'value', value, provenance: [{ kind: origin, source_id: 'fixture' }], ...(warnings ? { warnings } : {}) } } } };
    }
    return { draft, review, entry };
}

test('safe matching suggestions prefill once after preflight and retain corroborating sources', async () => {
    const f = fixture(); let checks = 0;
    const result = await f.review.prepare([f.entry('港湾', { beforeApply: async () => { checks++; } }), f.entry('港湾', { origin: 'ai', beforeApply: async () => { checks++; } }), f.entry('简介', { key: 'summary' })]);
    assert.equal(checks, 2); assert.deepEqual(result.appliedKeys.sort(), ['summary', 'title']);
    assert.equal(f.draft.getSnapshot().fields.title.value, '港湾'); assert.equal(f.draft.getSnapshot().fields.title.manual_locked, false);
    assert.equal(f.review.hasUnresolved(), false);
    assert.deepEqual(f.review.snapshot().prepared.find(row => row.key === 'title').sources.sort(), ['ai', 'ocr']);
});

test('different values and manual clears are kept for one explicit field decision', async () => {
    const f = fixture(); f.draft.clearField('summary');
    await f.review.prepare([f.entry('港湾'), f.entry('远山', { origin: 'ai' }), f.entry('新简介', { key: 'summary' })]);
    assert.equal(f.review.snapshot().pending.length, 2); assert.equal(f.draft.getSnapshot().fields.title, undefined);
    await f.review.resolve('title', 1); assert.equal(f.draft.getSnapshot().fields.title.value, '远山');
    await f.review.resolve('summary', 'keep'); assert.equal(f.draft.getSnapshot().fields.summary.state, 'cleared');
    assert.equal(f.review.hasUnresolved(), false);
});

test('manual edits during preparation, failed authority and aborted generations never overwrite', async () => {
    for (const mode of ['edit', 'reject', 'abort', 'clear']) {
        const f = fixture(); let finish; const controller = new AbortController();
        const preparing = f.review.prepare([f.entry('候选', { beforeApply: () => new Promise((resolve, reject) => { finish = mode === 'reject' ? () => reject(new Error('private-body')) : resolve; }) })], { signal: controller.signal });
        if (mode === 'edit') f.draft.setField('title', '人工');
        if (mode === 'abort') controller.abort();
        if (mode === 'clear') f.review.clear();
        finish(); const result = await preparing;
        assert.equal(f.draft.getSnapshot().fields.title?.value, mode === 'edit' ? '人工' : undefined, mode);
        assert.ok(!JSON.stringify(result).includes('private-body'));
    }
});

test('resolving a conflict rechecks settings and manual editing settles only that field', async () => {
    const f = fixture(); let allowed = true;
    await f.review.prepare([f.entry('甲', { beforeApply: async () => { if (!allowed) throw new Error('retired'); } }), f.entry('乙', { origin: 'ai' })]);
    allowed = false; await assert.rejects(f.review.resolve('title', 0), /校验/);
    assert.equal(f.draft.getSnapshot().fields.title, undefined); assert.equal(f.review.hasUnresolved(), true);
    f.draft.setField('title', '手填'); assert.equal(f.review.hasUnresolved(), false);
    assert.equal(f.draft.getSnapshot().fields.title.value, '手填');
});

test('a later source cannot erase earlier unresolved decisions and stale input cannot apply', async () => {
    const f = fixture(); await f.review.prepare([f.entry('甲'), f.entry('乙', { origin: 'ai' })]);
    await f.review.prepare([f.entry('简介', { key: 'summary' })]);
    assert.equal(f.review.snapshot().pending.length, 1);
    f.draft.setContext({ inputRevision: 3 });
    await assert.rejects(f.review.resolve('title', 0), /过期/);
    assert.equal(f.draft.getSnapshot().fields.title, undefined);
});

test('final confirmation rechecks only the suggestions still used in the document', async () => {
    const f = fixture(); let current = true; let calls = 0;
    await f.review.prepare([f.entry('星图', { beforeApply: async () => { calls++; if (!current) throw new Error('retired'); } })]);
    await f.review.preflight(); assert.equal(calls, 2);
    current = false;
    await assert.rejects(f.review.preflight(), /设置已变化/);
    assert.equal(f.draft.getSnapshot().fields.title.value, '星图');
    f.draft.setField('title', '人工核对');
    await f.review.preflight(); assert.equal(calls, 3);
    const controller = new AbortController(); controller.abort();
    await assert.rejects(f.review.preflight({ signal: controller.signal }), /重新确认/);
});

test('clearing comparisons retains adopted authority, and refreshed equal values replace the old check', async () => {
    const f = fixture(); let oldCurrent = true; let newCurrent = true;
    await f.review.prepare([f.entry('星图', { beforeApply: async () => { if (!oldCurrent) throw new Error('retired'); } })]);
    f.review.clear(); oldCurrent = false;
    await assert.rejects(f.review.preflight(), /设置已变化/);
    await f.review.prepare([f.entry('星图', { beforeApply: async () => { if (!newCurrent) throw new Error('retired'); } })]);
    await f.review.preflight();
    newCurrent = false;
    await assert.rejects(f.review.preflight(), /设置已变化/);
});

test('an explicitly adopted protected field refreshes equal authority without removing its manual lock', async () => {
    const f = fixture(); let oldCurrent = true; let newCurrent = true;
    f.draft.clearField('title');
    await f.review.prepare([f.entry('星图', { beforeApply: async () => { if (!oldCurrent) throw new Error('retired'); } })]);
    await f.review.resolve('title', 0);
    assert.equal(f.draft.getSnapshot().fields.title.manual_locked, true);
    f.review.clear(); oldCurrent = false;
    await f.review.prepare([f.entry('星图', { beforeApply: async () => { if (!newCurrent) throw new Error('retired'); } })]);
    await f.review.preflight();
    assert.equal(f.draft.getSnapshot().fields.title.manual_locked, true);
    newCurrent = false; await assert.rejects(f.review.preflight(), /设置已变化/);
    f.draft.setField('title', '人工核对'); await f.review.preflight();
});
