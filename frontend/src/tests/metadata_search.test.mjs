import test from 'node:test';
import assert from 'node:assert/strict';
import { createMetadataSearchModule } from '../metadata-search/index.js';
import { createMetadataDraft } from '../shared/metadata/draft.js';
import { createDocument, walk } from './metadata_dom_fixture.mjs';

const ok = payload => ({ response: { ok: true }, payload });
const tick = () => new Promise(resolve => setImmediate(resolve));
const sources = [
    { provider_id: 'mangabaka', enabled: true, priority: 1, config_version: 7, descriptor: { label: 'MangaBaka' }, custom_fields: [{ key: 'source.type', label: '来源作品类型', type: 'string' }], attribution: { label: 'MangaBaka', license: 'CC BY-NC-SA 4.0', license_url: 'https://mangabaka.org/about/data-license' } },
    { provider_id: 'bangumi', enabled: true, priority: 2, config_version: 8, descriptor: { label: 'Bangumi' }, custom_fields: [] },
    { provider_id: 'mangaupdates', enabled: false, priority: 3, config_version: 9, descriptor: { label: 'MangaUpdates' }, custom_fields: [] },
];
function fixture() {
    const doc = createDocument(); const root = doc.createElement('div');
    const field = { type: 'string', max_bytes: 100, enabled: true, editable: true, extractable: ['provider'] };
    const registry = { schema_version: 1, definitions_version: 'v1', definitions: { title: { ...field, key: 'title', label: '标题' }, 'custom.user.category': { ...field, key: 'custom.user.category', label: '分类' } } };
    const draft = createMetadataDraft({ definitions: registry.definitions, definitionsVersion: 'v1' });
    const requests = []; const displayed = []; const adoptionOptions = []; const reads = [];
    const api = { getJson: async (url, options) => { reads.push({ url, options }); return ok(url.endsWith('/schema') ? registry : { sources }); }, postJson: (url, input, options) => new Promise(resolve => requests.push({ url, input, options, resolve })) };
    let revision = 0;
    const module = createMetadataSearchModule({ root, doc, api, getDraft: () => draft, getRegistry: () => registry, showCandidate: (candidate, options) => { displayed.push(candidate); adoptionOptions.push(options); }, onInputChange: () => { draft.setContext({ inputRevision: ++revision, configRevision: 1 }); return draft.getContext(); } });
    const byId = id => walk(root).find(node => node.id === id);
    function edit(id, value, type = 'input') { const node = byId(id); if (node.type === 'checkbox') node.checked = value; else node.value = value; node.emit(type); }
    return { module, root, draft, requests, displayed, byId, edit, api, reads, adoptionOptions };
}
function resolveSearch(request) {
    request.resolve(ok({ query_revision: request.input.query_revision, sources: [
        { ...sources[0], candidates: [{ record_id: '123', title: 'Example', aliases: ['别名'], creators: { writer: ['作者'], penciller: ['画师'] }, relationship: 'series' }] },
        { ...sources[1], candidates: [], error: { code: 'rate_limited', retry_after: 60 } },
    ] }));
}
function resolveCandidate(request) {
    request.resolve(ok({ config_version: 7, candidate: { candidate_id: 'candidate', request_id: 'request', origin: 'provider', schema_version: 1, definitions_version: 'v1', base_document_revision: request.input.base_document_revision, input_revision: request.input.query_revision, config_revision: request.input.config_revision, field_revisions: { title: request.input.field_revisions.title || 0, 'custom.user.category': 0 }, fields: {
        title: { state: 'value', value: 'Example', provenance: [{ kind: 'provider', source_id: 'mangabaka', retrieved_at: '2026-10-05T00:00:00Z', source_field: 'title', attribution: 'MangaBaka CC BY-NC-SA 4.0', license_url: 'https://mangabaka.org/about/data-license' }] },
        'custom.user.category': { state: 'value', value: 'Doujinshi', provenance: [{ kind: 'provider', source_id: 'mangabaka' }] },
    } } }));
}
async function begin(f) {
    await f.module.mount(); f.edit('provider-keyword', 'Confirmed keyword'); f.edit('provider-select-mangabaka', true, 'change'); f.byId('provider-search-submit').emit('click');
}

test('selected source and explicit custom mapping resolve into shared preview without draft mutation', async () => {
    const f = fixture(); await f.module.mount(); await f.module.mount();
    assert.equal(f.byId('provider-select-mangaupdates').disabled, true);
    f.edit('provider-keyword', 'Confirmed keyword'); f.edit('provider-select-mangabaka', true, 'change'); f.edit('provider-mapping-custom.user.category', 'mangabaka:source.type', 'change');
    f.byId('provider-search-submit').emit('click'); assert.equal(f.requests.length, 1);
    assert.deepEqual(f.requests[0].input.provider_ids, ['mangabaka']); assert.equal(f.requests[0].input.keyword, 'Confirmed keyword'); assert.deepEqual(Object.keys(f.requests[0].input).sort(), ['keyword', 'provider_ids', 'query_revision']);
    resolveSearch(f.requests[0]); await tick();
    assert.ok(walk(f.root).some(node => node.textContent.includes('请求过于频繁')));
    const button = walk(f.root).find(node => node.textContent === '选择并核对字段'); assert.ok(button); assert.equal(f.displayed.length, 0);
    button.emit('click'); const request = f.requests[1]; assert.deepEqual(request.input.custom_mappings, { 'custom.user.category': 'source.type' });
    resolveCandidate(request); await tick(); assert.equal(f.displayed.length, 1); assert.deepEqual(f.draft.getSnapshot().fields, {});
    const candidate = f.displayed[0]; f.draft.applyCandidate(candidate, ['title', 'custom.user.category']);
    assert.equal(f.draft.getSnapshot().fields.title.provenance[0].source_field, 'title'); assert.equal(f.draft.getSnapshot().fields['custom.user.category'].value, 'Doujinshi'); assert.equal(f.draft.getSnapshot().fields.title.manual_locked, false);
    f.module.unmount(); assert.equal(f.root.children.length, 0);
});

test('old search response cannot render after keyword change or remount', async () => {
    const f = fixture(); await begin(f); const first = f.requests[0];
    f.edit('provider-keyword', 'New keyword'); assert.equal(first.options.signal.aborted, true);
    resolveSearch(first); await tick(); assert.equal(f.byId('provider-search-results').children.length, 0);
    f.byId('provider-search-submit').emit('click'); const second = f.requests[1]; f.module.unmount(); await f.module.mount(); resolveSearch(second); await tick();
    assert.equal(f.byId('provider-search-results').children.length, 0); assert.equal(f.displayed.length, 0);
});

test('manual edits, selected source changes and shared OCR context discard late details', async () => {
    for (const mutation of ['manual', 'source', 'context']) {
        const f = fixture(); await begin(f); resolveSearch(f.requests[0]); await tick();
        walk(f.root).find(node => node.textContent === '选择并核对字段').emit('click'); const detail = f.requests[1];
        if (mutation === 'manual') f.draft.setField('title', '手改');
        if (mutation === 'source') f.edit('provider-select-mangabaka', false, 'change');
        if (mutation === 'context') f.draft.setContext({ inputRevision: 500, configRevision: 2 });
        resolveCandidate(detail); await tick(); assert.equal(f.displayed.length, 0);
        if (mutation === 'manual') assert.equal(f.draft.getSnapshot().fields.title.value, '手改');
        f.module.unmount();
    }
});

test('unmount aborts provider loading and late hydration cannot recreate controls', async () => {
    const doc = createDocument(); const root = doc.createElement('div'); let complete; let signal;
    const module = createMetadataSearchModule({ root, doc, getDraft: () => null, showCandidate() {}, api: { getJson: (_url, options) => { signal = options.signal; return new Promise(resolve => { complete = resolve; }); } } });
    const loading = module.mount(); module.unmount(); complete(ok({ sources })); await loading;
    assert.equal(signal.aborted, true); assert.equal(root.children.length, 0);
});

async function displayedCandidate(f) {
    await begin(f); resolveSearch(f.requests[0]); await tick();
    walk(f.root).find(node => node.textContent === '选择并核对字段').emit('click');
    resolveCandidate(f.requests[1]); await tick();
    return f.adoptionOptions[0];
}

test('provider adoption preflight uses source version independently of the AI context and rechecks registry', async () => {
    for (const mutation of ['version', 'disabled', 'missing', 'schema', 'http', 'network', 'malformed', 'none']) {
        const f = fixture(); const options = await displayedCandidate(f);
        assert.equal(typeof options?.beforeApply, 'function'); assert.equal(f.reads.length, 1);
        assert.equal(f.displayed[0].config_revision, 1, 'AI context version differs from source version 7');
        const originalGet = f.api.getJson;
        f.api.getJson = async (...args) => {
            if (mutation === 'network') throw new Error('private response');
            const result = structuredClone(await originalGet(...args)); const url = args[0];
            if (mutation === 'http') result.response.ok = false;
            if (mutation === 'malformed') result.payload = null;
            if (mutation === 'schema' && url.endsWith('/schema')) result.payload.definitions_version = 'v2';
            if (url.endsWith('/sources')) {
                if (mutation === 'version') result.payload.sources[0].config_version++;
                if (mutation === 'disabled') result.payload.sources[0].enabled = false;
                if (mutation === 'missing') result.payload.sources = [];
            }
            return result;
        };
        const controller = new AbortController(); const before = f.draft.getSnapshot();
        if (mutation === 'none') {
            await options.beforeApply({ signal: controller.signal });
            assert.deepEqual(f.reads.slice(1).map(item => item.url).sort(), ['/api/settings/sources', '/api/metadata/schema'].sort());
            assert.ok(f.reads.slice(1).every(item => item.options.signal === controller.signal && item.options.cache === 'no-store'));
        } else await assert.rejects(options.beforeApply({ signal: controller.signal }));
        assert.deepEqual(f.draft.getSnapshot(), before); f.module.unmount();
    }
});

test('late provider authority checks cannot revive an invalidated or unmounted comparison', async () => {
    for (const mutation of ['input', 'unmount', 'abort']) {
        const f = fixture(); const options = await displayedCandidate(f); const originalGet = f.api.getJson; const completions = [];
        f.api.getJson = (...args) => new Promise(resolve => completions.push(async () => resolve(await originalGet(...args))));
        const controller = new AbortController(); const check = options.beforeApply({ signal: controller.signal }); const rejected = assert.rejects(check);
        if (mutation === 'input') f.edit('provider-keyword', 'Changed');
        if (mutation === 'unmount') f.module.unmount();
        if (mutation === 'abort') controller.abort();
        await Promise.all(completions.map(complete => complete())); await rejected;
        assert.equal(f.draft.getSnapshot().revision, 0); f.module.unmount();
    }
});
