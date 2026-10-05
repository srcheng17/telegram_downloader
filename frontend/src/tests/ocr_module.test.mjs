import test from 'node:test';
import assert from 'node:assert/strict';
import { mountOCR } from '../ocr/index.js';
import { createMetadataEditor } from '../shared/metadata/editor.js';
import { createMetadataDraft } from '../shared/metadata/draft.js';
import { ocrDocument, control, click, walk, tick } from './ocr_dom_fixture.mjs';

const schema = { schema_version: 1, definitions_version: 'standard-v1', definitions: { title: { key:'title', label:'标题', type:'string', enabled:true, editable:true, max_bytes:4096, extractable:['ai','rule'] } } };
function setup(post = async () => ({ response: {ok:false},payload:{code:'refused'} })) {
    const doc = ocrDocument(); const root = doc.createElement('div');
    const draft = createMetadataDraft({ definitions:schema.definitions,definitionsVersion:schema.definitions_version });
    const calls = []; const candidates = []; const candidateOptions = []; const reads = [];
    const api = { getJson:async (url, options) => { reads.push({url, options}); return ({ response:{ok:true},payload:url.endsWith('/schema') ? schema : url.endsWith('/ai') ? { config_version:1,enabled:true,base_url:'http://model.test/v1',model_id:'chosen-model' } : { rules_version:1,definitions_version:'standard-v1',rules:[{id:'title',target_key:'title',labels:['标题'],mode:'label_value'}] } }); }, postJson:(...args)=>{calls.push(args);return post(...args);} };
    const module = mountOCR(root,{draft,api,schema,onCandidates:(items,options)=>{candidates.push(items);candidateOptions.push(options);},queueOptions:{validate:async()=>({width:1,height:1}),createURL:()=> 'blob:test',revokeURL:()=>{}},recognizerFactory:()=>({recognize:async()=> '标题：图书',terminate(){}})});
    return {module,root,draft,calls,candidates,candidateOptions,api,reads,doc};
}
test('text paste stays native and model calls require explicit reviewed preview',async()=>{
    const {module,root,calls} = setup(); await tick();
    const paste = walk(root).find(node=>node.className==='ocr-paste-zone'); let prevented=0;
    for(const listener of paste.listeners.get('paste')) listener({clipboardData:{items:[{kind:'string',type:'text/plain'}]},preventDefault(){prevented++;}});
    assert.equal(prevented,0); assert.equal(calls.length,0);
    const manual = control(root,'补充或手工录入文字（加入合并预览）'); manual.value='标题：图书';manual.emit('input');
    click(root,'发送文字并提取'); assert.equal(calls.length,0);
    click(root,'从合并文字生成发送预览');click(root,'发送文字并提取');assert.equal(calls.length,0);
    const confirmed=control(root,'我已核对文字、服务目标、模型和字段，同意发送这份文字');confirmed.checked=true;
    click(root,'发送文字并提取');await tick();assert.equal(calls.length,1);assert.equal(calls[0][1].text,'标题：图书');assert.ok(!('base_url' in calls[0][1]));
    assert.match(walk(root).find(node=>node.attributes.role==='status').textContent,/拒绝/);module.dispose();
});
test('edited send snapshot is preserved on OCR change and late AI result is discarded',async()=>{
    let resolve;const {module,root,draft,calls,candidates}=setup(()=>new Promise(done=>{resolve=done;}));await tick();
    const sendText=control(root,'本次发送文字（可删改，只发送这一份）');sendText.value='Reviewed text';sendText.emit('input');
    control(root,'我已核对文字、服务目标、模型和字段，同意发送这份文字').checked=true;click(root,'发送文字并提取');
    await module.queue.add([{size:1}]);await tick();assert.equal(sendText.value,'Reviewed text');assert.equal(calls[0][2].signal.aborted,true);
    resolve({response:{ok:true},payload:{request_id:calls[0][1].request_id,candidates:[]}});await tick();assert.equal(draft.getSnapshot().revision,0);assert.ok(candidates.every(items=>items.length===0));
    assert.match(walk(root).filter(node=>node.tagName==='P').map(node=>node.textContent).join(' '),/保留你编辑/);module.dispose();assert.equal(root.children.length,0);assert.equal(sendText.value,'');
});
test('rules generate unapplied candidates and disposal cancels in-flight requests',async()=>{
    const {module,root,draft,candidates}=setup();await tick();
    const manual=control(root,'补充或手工录入文字（加入合并预览）');manual.value='标题：图书';manual.emit('input');click(root,'使用本地规则提取');click(root,'核对此候选');
    assert.equal(candidates.at(-1)[0].origin,'rule');assert.equal(draft.getSnapshot().revision,0);module.dispose();
});

async function prepareCandidate(f, origin) {
    await tick();
    const manual = control(f.root, '补充或手工录入文字（加入合并预览）'); manual.value = '标题：图书'; manual.emit('input');
    if (origin === 'rule') click(f.root, '使用本地规则提取');
    else {
        click(f.root, '从合并文字生成发送预览');
        control(f.root, '我已核对文字、服务目标、模型和字段，同意发送这份文字').checked = true;
        click(f.root, '发送文字并提取'); await tick();
    }
    click(f.root, '核对此候选');
    return { candidate: f.candidates.at(-1)[0], options: f.candidateOptions.at(-1) };
}
const validAI = async (_url, input) => ({ response: { ok: true }, payload: { request_id: input.request_id, candidates: [{
    candidate_id: 'ai-candidate', request_id: input.request_id, origin: 'ai', schema_version: 1, definitions_version: input.definitions_version,
    base_document_revision: input.base_document_revision, field_revisions: input.field_revisions, input_revision: input.input_revision, config_revision: input.config_revision,
    fields: { title: { state: 'value', value: '图书', provenance: [{ kind: 'ai', source_id: 'configured-model' }] } },
}] } });

test('rule and AI adoption reload current authority only when applying, and retain the draft on failure', async () => {
    for (const origin of ['rule', 'ai']) for (const mutation of ['version', 'schema', 'disabled', 'http', 'network', 'malformed', 'none']) {
        if (origin === 'rule' && mutation === 'disabled') continue;
        const f = setup(validAI); const { candidate, options } = await prepareCandidate(f, origin);
        assert.equal(typeof options?.beforeApply, 'function');
        assert.equal(f.reads.length, 2, 'opening comparison must not fetch or adopt');
        const originalGet = f.api.getJson;
        f.api.getJson = async (url, request) => {
            if (mutation === 'network') throw new Error('private-body');
            const result = structuredClone(await originalGet(url, request));
            if (mutation === 'http') result.response.ok = false;
            if (mutation === 'malformed') result.payload = null;
            if (mutation === 'schema' && url.endsWith('/schema')) result.payload.definitions_version = 'changed';
            if (mutation === 'version' && !url.endsWith('/schema')) result.payload[origin === 'rule' ? 'rules_version' : 'config_version']++;
            if (mutation === 'disabled' && url.endsWith('/ai')) result.payload.enabled = false;
            return result;
        };
        const host = f.doc.createElement('div'); const editor = createMetadataEditor({ root: host, draft: f.draft, schema, doc: f.doc }); editor.mount();
        editor.showCandidate(candidate, options);
        const panel = walk(host).find(node => node.className === 'metadata-candidates');
        walk(panel).find(node => node.type === 'checkbox').checked = true;
        click(host, '采用所选字段'); await tick();
        assert.equal(f.draft.getSnapshot().revision, mutation === 'none' ? 1 : 0, `${origin}/${mutation}`);
        if (mutation === 'none') {
            assert.deepEqual(f.reads.slice(2).map(item => item.url).sort(), ['/api/metadata/schema', origin === 'rule' ? '/api/settings/extraction-rules' : '/api/settings/ai'].sort());
            assert.ok(f.reads.slice(2).every(item => item.options.signal instanceof AbortSignal && item.options.cache === 'no-store'));
        }
        editor.unmount(); f.module.dispose();
    }
});

test('OCR comparison preflight rejects an input changed or disposed while authority replies late', async () => {
    for (const mutation of ['input', 'dispose', 'abort']) {
        const f = setup(); const { options } = await prepareCandidate(f, 'rule'); const controller = new AbortController();
        const originalGet = f.api.getJson; const completions = [];
        f.api.getJson = (...args) => new Promise(resolve => completions.push(async () => resolve(await originalGet(...args))));
        const check = options.beforeApply({ signal: controller.signal });
        const rejected = assert.rejects(check);
        if (mutation === 'input') control(f.root, '补充或手工录入文字（加入合并预览）').emit('input');
        if (mutation === 'dispose') f.module.dispose();
        if (mutation === 'abort') controller.abort();
        await Promise.all(completions.map(complete => complete())); await rejected;
        assert.equal(f.draft.getSnapshot().revision, 0); f.module.dispose();
    }
});
