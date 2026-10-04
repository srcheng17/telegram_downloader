import test from 'node:test';
import assert from 'node:assert/strict';
import { createExtractionRulesModule } from '../settings/extraction_rules.js';
import { ocrDocument, control, click, walk, tick } from './ocr_dom_fixture.mjs';
const schema={schema_version:1,definitions_version:'standard-v1',definitions:{title:{key:'title',label:'标题',type:'string',max_bytes:4096,enabled:true,extractable:['rule']}}};
const saved={rules_version:2,definitions_version:'standard-v1',rules:[{id:'title',target_key:'title',labels:['标题'],mode:'label_value'}]};
function setup(put=async()=>({response:{ok:true},payload:{...saved,rules_version:3}})){
 const doc=ocrDocument();const root=doc.createElement('div');doc.querySelector=()=>root;const calls=[];
 const api={getJson:async url=>({response:{ok:true},payload:url.endsWith('schema')?schema:saved}),putJson:(url,payload,options)=>{calls.push({url,payload,options});return put(url,payload,options);}};
 const module=createExtractionRulesModule({doc,api});module.mount();return{root,module,calls};
}
test('finite rule editor previews locally and saves explicit CAS/options without preview text',async()=>{
 const {root,module,calls}=setup();await tick();control(root,'示例文字（仅用于本页预览）').value='标题：合成测试';click(root,'预览当前规则');assert.match(walk(root).find(node=>node.tagName==='PRE').textContent,/合成测试/);assert.equal(calls.length,0);
 click(root,'保存规则');await tick();assert.equal(calls[0].payload.expected_version,2);assert.equal(calls[0].payload.rules[0].label_value_options.case_insensitive,true);assert.ok(!JSON.stringify(calls[0].payload).includes('合成测试'));module.unmount();assert.equal(root.children.length,0);
});
test('rule version conflict preserves local edits and unmount aborts pending save',async()=>{
 const {root,module,calls}=setup(async()=>({response:{ok:false,status:409},payload:{}}));await tick();const labels=control(root,'识别标签（每行一个，最多 8 个）');labels.value='书名';click(root,'保存规则');await tick();assert.equal(labels.value,'书名');assert.match(walk(root).find(node=>node.attributes.role==='status').textContent,/本页编辑保留/);assert.equal(calls.length,1);module.unmount();
 let complete;const other=setup(()=>new Promise(resolve=>{complete=resolve;}));await tick();click(other.root,'保存规则');other.module.unmount();assert.equal(other.calls[0].options.signal.aborted,true);complete({response:{ok:true},payload:saved});await tick();assert.equal(other.root.children.length,0);
});
