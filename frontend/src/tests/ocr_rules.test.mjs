import test from 'node:test';
import assert from 'node:assert/strict';
import { extractRules, validateRuleSet } from '../ocr/rules.js';
const defs = { title: { key:'title',label:'标题',type:'string',max_bytes:4096,enabled:true,extractable:['rule'] }, summary: { key:'summary',label:'简介',type:'string',max_bytes:16384,enabled:true,extractable:['rule'] }, tags: { key:'tags',label:'标签',type:'string[]',enabled:true,extractable:['rule'],max_items:64,item_max_bytes:1024 }, volume:{key:'volume',label:'卷',type:'integer',minimum:1,maximum:100,enabled:true,extractable:['rule']}, page_count:{key:'page_count',type:'integer',enabled:true,extractable:['archive']} };
const schema = { schema_version:1,definitions_version:'standard-v1',definitions:defs };
const rule=(id,key,label,mode='label_value')=>({id,target_key:key,labels:[label],mode});
const set=rules=>({rules_version:1,definitions_version:'standard-v1',rules});
const baseline={schema_version:1,definitions_version:'standard-v1',revision:0,fields:{}};
const run=(text,rules)=>extractRules({text,ruleSet:set(rules),schema,document:baseline,inputRevision:3,requestID:'rule-1'});
test('finite rules preserve roles, conflicts, continuation order and typed values',()=>{
 const got=run('标题：甲\n简介：第一段\n第二段\n卷：2\n标签：#星空 #探险\n标题：乙',[rule('title','title','标题'),rule('summary','summary','简介','continuation'),rule('volume','volume','卷'),rule('tags','tags','标签','hashtag_list')]);
 assert.equal(got.candidates.length,5);assert.deepEqual(got.candidates.find(c=>c.fields.tags).fields.tags.value,['星空','探险']);assert.equal(got.candidates.find(c=>c.fields.volume).fields.volume.value,2);assert.equal(got.candidates.find(c=>c.fields.summary).fields.summary.value,'第一段\n第二段');assert.ok(got.warnings.some(w=>w.code==='conflicting_values'));
});
test('invalid config, stale definitions, derived fields and label collisions never execute',()=>{
 for(const rules of [[rule('x','page_count','页数')],[rule('x','title','same'),rule('y','summary',' SAME ')],[rule('x','volume','卷','continuation')],[{...rule('x','title','标题'),regex:'.*'}]]) assert.throws(()=>validateRuleSet(set(rules),schema));
 assert.throws(()=>validateRuleSet({...set([]),definitions_version:'stale'},schema));
 const got=run('卷：未知\n标题：',[rule('volume','volume','卷'),rule('title','title','标题')]);assert.equal(got.candidates.length,0);
});
test('list splitting uses configured finite separators and evidence carries only references',()=>{
 const r={...rule('tags','tags','标签'),label_value_options:{separators:['semicolon'],case_insensitive:true,trim_space:true}};
 const got=run('标签：A, B; C',[r]);assert.deepEqual(got.candidates[0].fields.tags.value,['A, B','C']);
 const provenance=got.candidates[0].fields.tags.provenance[0];assert.equal(provenance.record_id,'rules:1:text:3');assert.equal(provenance.evidence.start,0);assert.equal(provenance.evidence.end,10);assert.ok(!('text' in provenance));
});
test('label case and whitespace normalization matches finite Go contract',()=>{
 const rules=[rule('one','title','İ'),rule('two','summary','i')];
 assert.doesNotThrow(()=>validateRuleSet(set(rules),schema));
 assert.equal(run('i：value',rules).candidates[0].fields.summary.value,'value');
 assert.throws(()=>validateRuleSet(set([{...rule('one','title','title'),labels:['title',' TITLE ']}]),schema));
 assert.throws(()=>validateRuleSet(set([{...rule('one','title','title'),label_value_options:null}]),schema));
 assert.equal(run('\u3000Title\u3000：value',[rule('one','title','title')]).candidates[0].fields.title.value,'value');
 assert.equal(run('i\u0307：value',[rule('one','title','İ')]).candidates.length,0);
 assert.equal(run('\uFEFFtitle：value',[rule('one','title','title')]).candidates.length,0);
});
