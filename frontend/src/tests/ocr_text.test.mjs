import test from 'node:test';
import assert from 'node:assert/strict';
import { deriveOCRText, extractLocalText, remapEvidence } from '../ocr/text.js';

const definitions = Object.fromEntries(['title','aliases','creators.writer','creators.translator','summary','tags'].map(key => [key, { key, label:key, type:['title','summary'].includes(key)?'string':'string[]', enabled:true, editable:true, extractable:['ocr','rule','ai'], max_bytes:4096, item_max_bytes:1024, max_items:64 }]));
const schema = { schema_version:1, definitions_version:'test', definitions };
const ruleSet = { rules_version:1, definitions_version:'test', rules:[{ id:'summary',target_key:'summary',labels:['简介'],mode:'continuation' }] };
const options = text => ({ text, schema, ruleSet, document:{revision:0,fields:{}}, inputRevision:1,configRevision:1 });
const synthetic = '《星 海 旅 记 》\n剧情 介绍 :\n旅人于 12:30 出发（第 2 次）。\n他们找到星图。\n\n#奇 幻 #冒 险\n👍 12\n👁 250 18:40\nLeave a Comment\n[River Ink]Star Jour...zip\n40.5 MB\n[River Ink]Star Journey(Aster_x_Beryl)[星 光 汉 化]\n[#River Ink]星 海 旅 记（阿斯特×贝丽尔）[#星 光 汉 化]';
test('finite caption extraction keeps four identities and excludes roles and chat UI', () => {
    const result = extractLocalText(options(synthetic));
    const fields = Object.assign({}, ...result.candidates.map(candidate => candidate.fields));
    assert.equal(fields.title.value, '星海旅记');
    assert.deepEqual(fields.aliases.value, ['Star Journey']);
    assert.deepEqual(fields['creators.writer'].value, ['River Ink']);
    assert.deepEqual(fields['creators.translator'].value, ['星光汉化']);
    assert.equal(fields.summary.value, '旅人于 12:30 出发（第 2 次）。\n他们找到星图。');
    assert.deepEqual(fields.tags.value, ['奇幻','冒险']);
    assert.ok(!fields.series);
    assert.ok(!JSON.stringify(fields).includes('Leave a Comment'));
    assert.equal(result.derived.rawText, synthetic);
    for (const candidate of result.candidates) for (const field of Object.values(candidate.fields)) for (const provenance of field.provenance) {
        assert.ok(provenance.evidence.end > provenance.evidence.start);
        assert.ok(provenance.evidence.end <= synthetic.length);
    }
});
test('normalization preserves English names, meaningful numbers and punctuation with UTF16 mapping', () => {
    const raw = '😀 简 介：River Ink at 12:30（第 2 次）。\n星 海';
    const derived = deriveOCRText(raw);
    assert.equal(derived.text, '😀 简介：River Ink at 12:30（第 2 次）。\n星海');
    const start = derived.text.indexOf('星海');
    assert.deepEqual(remapEvidence(derived,{start,end:start+2}),{start:raw.indexOf('星 海'),end:raw.length});
});
test('summary rules stop at paragraph, hashtag and image boundaries without dropping legitimate times', () => {
    const text='简介：在 12:30 出发\n第 2 天到达\n12:45 继续旅程\n到 13:00 休息\n\n#旅行\n新气泡';
    const result=extractLocalText(options(text));
    assert.equal(result.candidates.find(candidate=>candidate.origin==='rule').fields.summary.value,'在 12:30 出发\n第 2 天到达\n12:45 继续旅程\n到 13:00 休息');
});
test('ambiguous brackets and truncated names never invent writer/title/series', () => {
    for(const text of ['[Aster_x_Beryl]Star Journey.zip','[River Ink]Star Jour...zip','[备注]正文（角色）']) {
        const result=extractLocalText(options(text));
        assert.equal(result.candidates.length,0);
    }
});
test('multiple distinct captions remain conflicting candidates and image evidence is local',()=>{
    const first='《星 海》'; const second='《山 川》'; const text=first+'\n\n'+second;
    const result=extractLocalText({...options(text),segments:[{image_id:'a',text_revision:1,start:0,end:first.length},{image_id:'b',text_revision:2,start:first.length+2,end:text.length}]});
    assert.deepEqual(result.candidates.map(item=>item.fields.title.value),['星海','山川']);
    assert.deepEqual(result.candidates[1].fields.title.provenance[0].evidence,{image_id:'b',start:0,end:second.length});
});

test('wrapped bilingual captions join only within the caption and retain original evidence',()=>{
    const text='《星 海 旅 记》\n[River Ink]Star\nJourney(Aster_x_Beryl)[星 光 汉化]\n[#River Ink]星 海 旅 记（阿斯\n特 x 贝丽尔）[#星 光 汉化]\n儿 人 10.5K 12:05\n后 Leave a Comment > 省\n曲 River...星光汉化.zip';
    const result=extractLocalText(options(text));const fields=Object.assign({},...result.candidates.map(item=>item.fields));
    assert.deepEqual(fields.aliases.value,['Star Journey']);assert.deepEqual(fields['creators.writer'].value,['River Ink']);assert.deepEqual(fields['creators.translator'].value,['星光汉化']);assert.equal(fields.title.value,'星海旅记');
    assert.ok(!result.derived.text.includes('10.5K'));assert.ok(!result.derived.text.includes('Comment'));assert.ok(!result.derived.text.includes('.zip'));
    assert.ok(result.candidates.every(candidate=>Object.values(candidate.fields).every(field=>field.provenance.every(source=>source.evidence.end<=text.length))));
});
test('standalone legitimate time and number remain in non-UI content',()=>{
    assert.equal(deriveOCRText('12:30\n1984\n第 2 卷').text,'12:30\n1984\n第 2 卷');
});

test('joined caption whitespace evidence maps through earlier removed OCR spaces', () => {
    const raw = '《星 海》\n[River Ink]Star\nJourney[星 光 汉 化]';
    const derived = deriveOCRText(raw);
    const start = derived.text.indexOf('Star Journey') + 'Star'.length;
    const newline = raw.indexOf('\nJourney');
    assert.deepEqual(remapEvidence(derived, { start, end: start + 1 }), { start: newline, end: newline + 1 });
});
