import { byteLength, validateFieldValue } from '../shared/metadata/schema.js';
import { extractRules } from './rules.js';

// Local derived text only. Offsets always map back to the unchanged edited OCR
// input (which is itself kept separately from the recognizer's raw image text).
const han = /\p{Script=Han}/u;
const truncated = /…|\.{3}/u;
const caption = text => text.match(/^\[\s*#?([^[\]]{1,100})\]\s*([^[\]]+?)\s*\[\s*#?([^[\]]*(?:汉化|漢化|翻译|翻譯)[^[\]]*)\]\s*$/u);
const uiLine = text => {
    const line=text.trim();
    return (line.length < 80 && /Leave a Comment/iu.test(line)) ||
        /^(?:\d+\s*(?:comments?|条评论)|\d+(?:\.\d+)?\s*(?:KB|MB|GB)|(?:👁\ufe0f?|◉)\s*[\d.,Kk]+(?:\s+\d{1,2}:\d{2})?|(?:👍|❤️?|🔥|👏|😁)+\s*\d+(?:\s+(?:👍|❤️?|🔥|👏|😁)+\s*\d+)*)$/iu.test(line) ||
        (line.length < 80 && /\b\d+(?:\.\d+)?[Kk]\s+\d{1,2}:\d{2}\s*$/u.test(line)) ||
        (truncated.test(line) && /\.\s*(?:zip|rar|7z|cbz)\s*$/iu.test(line));
};

function joinCaptionLines(text,mapping) {
    const lines=[];let offset=0;
    for(const value of text.split('\n')) {lines.push({value,start:offset,end:offset+value.length});offset+=value.length+1;}
    let joined='';const joinedMap=[];
    for(let i=0;i<lines.length;i++) {
        let value=lines[i].value;let map=mapping.slice(lines[i].start,lines[i].end);let end=i;
        if(/^\s*\[\s*#?[^\]]+\]/u.test(value)&&!caption(value)) {
            for(let next=i+1;next<Math.min(i+4,lines.length);next++) {
                const line=lines[next];if(!line.value.trim()||/^\s*\[/u.test(line.value))break;
                const previous=value.trimEnd();const following=line.value.trimStart();
                const join=han.test(previous.slice(-1))&&han.test(following[0]||'')?'':' ';
                const skipped=line.value.length-following.length;
                map=map.slice(0,previous.length);
                if(join)map.push(mapping[lines[next-1].end]);
                map.push(...mapping.slice(line.start+skipped,line.end));
                value=previous+join+following;
                if(caption(value)) {end=next;break;}
            }
        }
        if(end===i) {value=lines[i].value;map=mapping.slice(lines[i].start,lines[i].end);}
        joined+=value;joinedMap.push(...map);i=end;
        if(i<lines.length-1) {joined+='\n';joinedMap.push(mapping[lines[i].end]);}
    }
    return {text:joined,mapping:joinedMap};
}

export function deriveOCRText(rawText) {
    if (typeof rawText !== 'string' || byteLength(rawText) > 65536) throw new Error('识别文字不能超过 64 KiB。');
    const mapping = []; let text = ''; let offset = 0;
    const removed = [];
    for (const rawLine of rawText.split('\n')) {
        let normalized = ''; const lineMap = [];
        for (let i = 0; i < rawLine.length; i++) {
            if (/[ \t\r]/u.test(rawLine[i])) {
                let next = i; while (next < rawLine.length && /[ \t\r]/u.test(rawLine[next])) next++;
                const previous = rawLine[i - 1]; const following = rawLine[next];
                // Only horizontal whitespace between Han letters, or inside
                // Chinese book-title punctuation. Never join separate lines.
                if ((han.test(previous || '') && han.test(following || '')) || (previous === '《' && han.test(following || '')) || (han.test(previous || '') && following === '》')) { i = next - 1; continue; }
            }
            normalized += rawLine[i]; lineMap.push(offset + i);
        }
        if (uiLine(normalized)) { removed.push({ start:offset,end:offset+rawLine.length }); normalized = ''; lineMap.length = 0; }
        text += normalized; mapping.push(...lineMap);
        offset += rawLine.length;
        if (offset < rawText.length) { text += '\n'; mapping.push(offset); offset++; }
    }
    return { rawText, ...joinCaptionLines(text,mapping), removed };
}

export function remapEvidence(derived, evidence, segments = []) {
    if (!Number.isSafeInteger(evidence?.start) || !Number.isSafeInteger(evidence?.end) || evidence.start < 0 || evidence.end <= evidence.start || evidence.end > derived.mapping.length) throw new Error('识别证据范围无效。');
    const start = derived.mapping[evidence.start]; const end = derived.mapping[evidence.end - 1] + 1;
    const segment = segments.find(item => start >= item.start && end <= item.end);
    return segment ? { image_id:segment.image_id,start:start-segment.start,end:end-segment.start } : { start,end };
}

export function mapCandidateEvidence(candidates, derived, segments = []) {
    return candidates.map(candidate => ({ ...candidate, fields:Object.fromEntries(Object.entries(candidate.fields).map(([key,field]) => [key,{ ...field,provenance:field.provenance.map(source => source.evidence ? { ...source,evidence:remapEvidence(derived,source.evidence,segments) } : source) }])) }));
}

export function extractLocalText({ text, segments = [], ruleSet, schema, document, inputRevision, configRevision, requestID = `ocr-${crypto.randomUUID()}` }) {
    const derived = deriveOCRText(text); const lines = []; let start = 0;
    for (const line of derived.text.split('\n')) { lines.push({ text:line,start,end:start+line.length }); start += line.length + 1; }
    const boundaryOffsets = new Set(lines.filter(line => !line.text.trim() || /^\s*#/u.test(line.text) || caption(line.text)).map(line => line.start));
    const result = ruleSet ? extractRules({ text:derived.text, ruleSet,schema,document,inputRevision,configRevision,requestID:`${requestID}-rules`,boundaryOffsets }) : {candidates:[],warnings:[]};
    result.candidates = mapCandidateEvidence(result.candidates,derived,segments);
    const seen = new Map();
    function add(key,value,line,end=line.end) {
        const definition = schema.definitions[key];
        if (!definition?.enabled || !definition.extractable?.includes('ocr')) return;
        try { validateFieldValue(definition,value); } catch { return; }
        const signature=JSON.stringify(value); const values=seen.get(key)||new Set(); if(values.has(signature))return;
        values.add(signature);seen.set(key,values);
        const evidence=remapEvidence(derived,{start:line.start,end},segments);
        result.candidates.push({ candidate_id:`${requestID}-${result.candidates.length}`,request_id:requestID,origin:'ocr',schema_version:schema.schema_version,definitions_version:schema.definitions_version,base_document_revision:document.revision,input_revision:inputRevision,...(configRevision===undefined?{}:{config_revision:configRevision}),field_revisions:{[key]:document.fields[key]?.revision||0},fields:{[key]:{state:'value',value,provenance:[{kind:'ocr',source_id:'local-caption-v1',record_id:`text:${inputRevision}`,evidence}]}} });
    }
    for (let i=0;i<lines.length;i++) {
        const line=lines[i];const value=line.text.trim();
        const book=value.match(/^《([^《》\n]{1,300})》$/u);
        if(book&&!truncated.test(book[1]))add('title',book[1].trim(),line);
        const parts=caption(value);
        if(parts&&!truncated.test(value)) {
            const writer=parts[1].trim(); const translator=parts[3].trim();
            const title=parts[2].replace(/\s*[（(][^()（）]*[)）]\s*$/u,'').trim();
            // Require the translation marker + a complete title, not arbitrary
            // bracketed prose. Pairing syntax cannot become an author.
            if(title&&!/[_×]|\bx\b/u.test(writer)&&!/[()（）]/u.test(writer)) {
                add(han.test(title)?'title':'aliases',han.test(title)?title:[title],line);
                add('creators.writer',[writer],line);add('creators.translator',[translator],line);
            }
        }
        if (/^(?:剧情介绍|劇情介紹)\s*[:：]/u.test(value)) {
            let summary=value.replace(/^(?:剧情介绍|劇情介紹)\s*[:：]\s*/u,'');let end=line.end;
            for(let next=i+1;next<lines.length;next++) {
                const item=lines[next];if(boundaryOffsets.has(item.start)||/^\s*[^:：\n]{1,128}[:：]/u.test(item.text.replace(/\b\d{1,2}:\d{2}\b/gu,'')))break;
                summary+=(summary?'\n':'')+item.text;end=item.end;
            }
            if(summary.trim())add('summary',summary.trim(),line,end);
        }
        if (/^\s*#[^#]+(?:\s+#[^#]+)*\s*$/u.test(value)) {
            const tags=[...value.matchAll(/#([^#]+)/gu)].map(match=>match[1].trim()).filter(Boolean);
            if(tags.length)add('tags',tags,line);
        }
    }
    if(result.candidates.length>64)throw new Error('匹配到的建议过多，请缩小文字范围后重试。');
    return {...result,derived};
}
