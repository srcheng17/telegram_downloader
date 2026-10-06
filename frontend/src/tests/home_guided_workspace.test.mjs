import test from 'node:test';
import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
import { createGuidedWorkspace } from '../home/guided_workspace.js';
import { createDocument } from './metadata_dom_fixture.mjs';

function fixture() {
    const doc = createDocument(); doc.addEventListener = () => {}; doc.removeEventListener = () => {}; const nodes = new Map(); const calls = [];
    const node = selector => { if (!nodes.has(selector)) nodes.set(selector, doc.createElement('div')); return nodes.get(selector); };
    const panels = ['materials', 'preparing', 'review', 'result'].map(step => { const item = node(`[panel-${step}]`); item.dataset.workflowStep = step; return item; });
    const root = { dataset: {}, querySelector: node, querySelectorAll(selector) { if (selector === '[data-workflow-step]') return panels; if (selector === '[data-workflow-indicator]') return []; return selector.split(', ').map(node); } };
    node('#download-form').querySelector = () => null;
    node('#automatic-preparation').checked = true; node('#delivery-target').value = 'download'; node('#url').value = 'https://telegra.ph/fictional-test';
    doc.querySelector = () => ({ value: 'url' });
    const current = { schema_version: 1, definitions_version: 'v1', revision: 0, fields: {} };
    const draft = { getSnapshot: () => structuredClone(current), getContext: () => ({ inputRevision: 1 }) };
    let unresolved = false; let prepared = 0; let searched = 0;
    const shell = { getDraft: () => draft, getDocument: () => draft.getSnapshot(), hasUnresolvedCandidates: () => unresolved, async prepareCandidates(entries) { calls.push(['candidates', entries.length]); current.fields.title = { state: 'value', value: '虚构样例' }; return { warnings: [] }; } };
    const api = { async getJson(url) { calls.push(['get', url]); return { response: { ok: true }, payload: { definitions_version: 'v1' } }; } };
    const ocr = { async prepare() { prepared += 1; return { entries: [{ candidate: {} }], warnings: [] }; }, getInputRevision: () => 1, cancelPreparation() {}, markClean() {} };
    const search = { async prepare(options) { searched += 1; assert.equal(options.title, '虚构样例'); return { entries: [], warnings: [] }; } };
    const guide = createGuidedWorkspace({ root, doc, win: { crypto: webcrypto }, api, getShell: () => shell, getOCR: () => ocr, getSearch: () => search, showFeedback() {} });
    return { guide, root, doc, node, panels, current, calls, api, ocr, shell, setUnresolved: value => { unresolved = value; }, counts: () => ({ prepared, searched }) };
}
test('materials automatically prepare one review draft without task creation; returning keeps content and focus follows the step', async () => {
    const f = fixture(); await f.guide.workflow.next();
    assert.equal(f.root.dataset.workflowStep, 'review'); assert.equal(f.doc.activeElement, f.node('#workflow-review-title'));
    assert.equal(f.panels.find(panel => panel.dataset.workflowStep === 'materials').hidden, true);
    assert.deepEqual(f.calls, [['candidates', 1]]); assert.equal(f.current.fields.title.value, '虚构样例');
    f.guide.workflow.back(); await f.guide.workflow.next(); assert.deepEqual(f.counts(), { prepared: 1, searched: 1 });
    f.guide.dispose();
});
test('automatic preparation can be disabled without sending OCR text or search terms', async () => {
    const f = fixture(); f.node('#automatic-preparation').checked = false; await f.guide.workflow.next();
    assert.equal(f.guide.workflow.step, 'review'); assert.deepEqual(f.counts(), { prepared: 0, searched: 0 }); assert.deepEqual(f.calls, []);
    f.guide.dispose();
});
test('unresolved conflict blocks final confirmation and creates no upload or download task', async () => {
    const f = fixture(); await f.guide.workflow.next(); f.setUnresolved(true);
    await assert.rejects(f.guide.confirm(new AbortController().signal), /冲突/);
    assert.equal(f.guide.workflow.submission, null); assert.equal(f.calls.filter(([type]) => type === 'get').length, 0);
    f.guide.dispose();
});
test('source or metadata change during final preflight invalidates confirmation before submission', async () => {
    const f = fixture(); await f.guide.workflow.next(); let finish;
    f.api.getJson = () => new Promise(resolve => { finish = resolve; });
    const confirmation = f.guide.confirm(new AbortController().signal);
    assert.equal(f.node('[data-workflow-editable]').disabled, true);
    f.current.revision += 1; finish({ response: { ok: true }, payload: { definitions_version: 'v1' } });
    await assert.rejects(confirmation, /已变化/); assert.equal(f.guide.workflow.busy, false); assert.equal(f.node('[data-workflow-editable]').disabled, false);
    f.guide.dispose();
});
test('late preparation after return cannot take focus or reveal the review screen', async () => {
    const f = fixture(); let finish; f.ocr.prepare = () => new Promise(resolve => { finish = resolve; });
    const waiting = f.guide.workflow.next(); f.guide.workflow.back(); finish({ entries: [{ candidate: {} }], warnings: [] }); await waiting;
    assert.equal(f.root.dataset.workflowStep, 'materials'); assert.equal(f.doc.activeElement, f.node('#workflow-materials-title')); assert.deepEqual(f.calls, []);
    f.guide.dispose();
});

test('final confirmation reruns adopted suggestion authority and preserves the draft on failure', async () => {
    const f = fixture(); await f.guide.workflow.next(); let preflights = 0;
    f.shell.preflightPreparedCandidates = async ({ signal }) => { assert.equal(signal.aborted, false); preflights += 1; throw new Error('saved configuration changed'); };
    await assert.rejects(f.guide.confirm(new AbortController().signal), /设置校验/);
    assert.equal(preflights, 1); assert.equal(f.current.fields.title.value, '虚构样例');
    assert.equal(f.guide.workflow.step, 'review'); assert.equal(f.guide.workflow.busy, false); assert.equal(f.node('[data-workflow-retry]').hidden, false);
    f.guide.dispose();
});

test('existing www Telegraph and graph.org URLs stay valid in the guided flow', async () => {
    const f = fixture(); await f.guide.workflow.next();
    for (const host of ['www.telegra.ph', 'graph.org', 'www.graph.org']) {
        f.node('#url').value = `https://${host}/sample`;
        const attempt = await f.guide.confirm(new AbortController().signal); assert.equal(attempt.snapshot.source.url, f.node('#url').value); f.guide.failed();
    }
    f.guide.dispose();
});
