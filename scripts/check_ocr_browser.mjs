// Dedicated real-WASM acceptance check; fixture text is entirely synthetic.
import { chromium } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { extractLocalText } from '../frontend/src/ocr/text.js';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const fixtures = path.join(root, 'tests/e2e/fixtures/ocr');
await mkdir(fixtures, { recursive: true });
const types = { '.js': 'text/javascript', '.wasm': 'application/wasm', '.png': 'image/png', '.gz': 'application/gzip' };
const server = createServer(async (req, res) => {
    const name = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
    if (name === '/') { res.setHeader('Content-Type', 'text/html'); res.end('<!doctype html><html lang="zh"><meta charset="UTF-8"><title>本地 OCR 验证</title><body></body></html>'); return; }
    const target = path.resolve(root, '.' + name);
    if (!target.startsWith(root + path.sep)) { res.writeHead(403).end(); return; }
    try { const data = await readFile(target); res.setHeader('Content-Type', types[path.extname(target)] || 'application/octet-stream'); res.setHeader('Cache-Control', 'public, max-age=31536000'); res.end(data); }
    catch { res.writeHead(404).end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({ headless: true });
const report = { synthetic: true, engine: 'Tesseract.js 7.0.0', results: [], externalRequests: [], notes: ['Desktop Chromium only; mobile memory peak and user screenshot accuracy remain separate checks.'] };
const cases = [
    { language: 'eng', lines: ['Title: Star Atlas', 'Writer: Example Author', 'Volume: 2'] },
    { language: 'chi_sim', lines: ['标题：星空图书', '作者：示例作者', '简介：这是一个关于图书的故事。'] },
    { language: 'chi_tra', lines: ['標題：星空圖書', '作者：範例作者', '簡介：這是一個關於圖書的故事。'] },
    { language: 'jpn', lines: ['タイトル：星空の本', '作者：山田太郎', 'これは図書館の物語です。'] },
];
try {
    const context = await browser.newContext(); const page = await context.newPage();
    context.on('request', request => { if (!request.url().startsWith(origin) && !request.url().startsWith('blob:')) report.externalRequests.push(request.url()); });
    await page.goto(origin);
    for (const sample of cases) {
        const result = await page.evaluate(async ({ sample }) => {
            const canvas = document.createElement('canvas'); canvas.width = 1300; canvas.height = 360;
            const ctx = canvas.getContext('2d'); ctx.fillStyle = 'white'; ctx.fillRect(0, 0, canvas.width, canvas.height); ctx.fillStyle = 'black'; ctx.font = '44px sans-serif'; sample.lines.forEach((line, index) => ctx.fillText(line, 35, 85 + index * 95));
            const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
            const { validateImage } = await import('/frontend/src/ocr/images.js'); await validateImage(new File([blob], 'synthetic.png', { type: 'image/png' }));
            const { createLocalRecognizer } = await import('/frontend/src/ocr/recognizer.js');
            const recognizer = createLocalRecognizer({ resourceRoot: '/web/static/ocr/tesseract-7.0.0' });
            const started = performance.now();
            try { const text = await recognizer.recognize(blob, [sample.language], () => {}); return { text, elapsedMs: Math.round(performance.now() - started), png: canvas.toDataURL('image/png').split(',')[1] }; }
            finally { recognizer.terminate(); }
        }, { sample });
        const fixture = path.join(fixtures, sample.language + '.png');
        // Keep the established white fixtures stable across OS font rendering.
        try { await readFile(fixture); } catch { await writeFile(fixture, Buffer.from(result.png, 'base64')); }
        if (!result.text.trim()) throw new Error('Empty real OCR output: ' + sample.language);
        const normalize = text => text.replace(/\s/gu, ''); const expected = normalize(sample.lines.join('\n')); const actual = normalize(result.text);
        let row = Array.from({ length: actual.length + 1 }, (_, i) => i);
        for (let i = 1; i <= expected.length; i++) { const next = [i]; for (let j = 1; j <= actual.length; j++) next[j] = Math.min(next[j - 1] + 1, row[j] + 1, row[j - 1] + (expected[i - 1] === actual[j - 1] ? 0 : 1)); row = next; }
        report.results.push({ language: sample.language, expected: sample.lines.join('\n'), actual: result.text, elapsedMs: result.elapsedMs, characterEditsIgnoringWhitespace: row[actual.length], expectedCharactersIgnoringWhitespace: expected.length });
    }
    const chatLines = [
        '《星 海 旅 记》',
        '剧情 介绍 :',
        '旅人于 12:30 出发（第 2 次）。',
        '他们找到星图。',
        '',
        '#奇 幻 #冒 险',
        '👍 12',
        '👁 250 18:40',
        'Leave a Comment',
        '[River Ink]Star Jour...zip',
        '40.5 MB',
        '',
        '[River Ink]Star Journey(Aster_x_Beryl)[星 光 汉 化]',
        '[#River Ink]星 海 旅 记（阿斯特×贝丽尔）[#星 光 汉 化]',
    ];
    const darkChat = await page.evaluate(async lines => {
        const canvas = document.createElement('canvas'); canvas.width = 1600; canvas.height = 1200;
        const ctx = canvas.getContext('2d');
        ctx.fillStyle = '#101820'; ctx.fillRect(0, 0, canvas.width, canvas.height);
        ctx.fillStyle = '#202d3b'; ctx.fillRect(28, 24, 1544, 950);
        ctx.fillStyle = '#27384a'; ctx.fillRect(28, 990, 1544, 182);
        ctx.font = '40px sans-serif'; ctx.textBaseline = 'top';
        lines.forEach((line, index) => {
            ctx.fillStyle = index === 5 ? '#85c9ed' : index >= 6 && index <= 10 ? '#c0cbd5' : '#f1f5f8';
            ctx.fillText(line, 58, 56 + index * 80);
        });
        const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
        const { validateImage } = await import('/frontend/src/ocr/images.js');
        await validateImage(new File([blob], 'synthetic-dark-chat.png', { type: 'image/png' }));
        const { createLocalRecognizer } = await import('/frontend/src/ocr/recognizer.js');
        const recognizer = createLocalRecognizer({ resourceRoot: '/web/static/ocr/tesseract-7.0.0' });
        const started = performance.now();
        try {
            const unprocessed = createLocalRecognizer({ resourceRoot: '/web/static/ocr/tesseract-7.0.0', prepareImage: file => file });
            let rawText;
            try { rawText = await unprocessed.recognize(blob, ['chi_sim', 'eng'], () => {}); }
            finally { unprocessed.terminate(); }
            const text = await recognizer.recognize(blob, ['chi_sim', 'eng'], () => {});
            const elapsedMs = Math.round(performance.now() - started);
            const png = canvas.toDataURL('image/png').split(',')[1];
            // A controlled same-pixel comparison, separate from the default path.
            const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height);
            for (let i = 0; i < pixels.data.length; i += 4) {
                const gray = (pixels.data[i] + pixels.data[i + 1] + pixels.data[i + 2]) / 3;
                const value = gray > 110 ? 0 : 255;
                pixels.data[i] = pixels.data[i + 1] = pixels.data[i + 2] = value;
            }
            ctx.putImageData(pixels, 0, 0);
            const binary = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
            const binaryStarted = performance.now();
            const binaryText = await recognizer.recognize(binary, ['chi_sim', 'eng'], () => {});
            return { text, rawText, elapsedMs, png, binaryText, binaryElapsedMs: Math.round(performance.now() - binaryStarted) };
        }
        finally { recognizer.terminate(); }
    }, chatLines);
    await writeFile(path.join(fixtures, 'synthetic-dark-chat.png'), Buffer.from(darkChat.png, 'base64'));
    const definitions = Object.fromEntries(['title', 'aliases', 'creators.writer', 'creators.translator', 'summary', 'tags'].map(key => [key, {
        key, label: key, type: ['title', 'summary'].includes(key) ? 'string' : 'string[]', enabled: true,
        extractable: ['ocr'], max_bytes: 4096, item_max_bytes: 1024, max_items: 64,
    }]));
    const extracted = extractLocalText({ text: darkChat.text, schema: { schema_version: 1, definitions_version: 'synthetic-chat-v1', definitions }, document: { revision: 0, fields: {} }, inputRevision: 1 });
    const fieldValues = Object.fromEntries(extracted.candidates.flatMap(candidate => Object.entries(candidate.fields).map(([key, field]) => [key, field.value])));
    const expectedFields = { title: '星海旅记', aliases: ['Star Journey'], 'creators.writer': ['River Ink'], 'creators.translator': ['星光汉化'] };
    const coreFields = Object.fromEntries(Object.entries(expectedFields).map(([key, expected]) => [key, { expected, actual: fieldValues[key] ?? null, matches: JSON.stringify(fieldValues[key]) === JSON.stringify(expected) }]));
    const serialized = JSON.stringify(fieldValues);
    const roleMisclassification = /Aster|Beryl|阿斯特|贝丽尔/.test(JSON.stringify({ writer: fieldValues['creators.writer'], translator: fieldValues['creators.translator'], series: fieldValues.series }));
    const noiseLeakage = /Leave a Comment|40\.5|Star Jour\.\.\.|18:40|250|👍|👁/.test(serialized);
    const summaryBounded = Boolean(fieldValues.summary) && !/#|Comment|Journey|River|汉化/.test(fieldValues.summary);
    report.darkChat = { fixture: 'tests/e2e/fixtures/ocr/synthetic-dark-chat.png', expectedText: chatLines.join('\n'), actualText: darkChat.text, elapsedMs: darkChat.elapsedMs, coreFields, fields: fieldValues, roleMisclassification, noiseLeakage, summaryBounded };
    report.darkChat.passed = Object.values(coreFields).every(field => field.matches) && !roleMisclassification && !noiseLeakage && summaryBounded;
    const binaryExtracted = extractLocalText({ text: darkChat.binaryText, schema: { schema_version: 1, definitions_version: 'synthetic-chat-v1', definitions }, document: { revision: 0, fields: {} }, inputRevision: 1 });
    const binaryFields = Object.fromEntries(binaryExtracted.candidates.flatMap(candidate => Object.entries(candidate.fields).map(([key, field]) => [key, field.value])));
    report.darkChat.samePixelBinaryComparison = {
        method: 'Same-pixel grayscale mean > 110 becomes black; otherwise white. Default runtime selects this only for clearly dark backgrounds.',
        actualText: darkChat.binaryText, elapsedMs: darkChat.binaryElapsedMs, fields: binaryFields,
        coreFieldsMatched: Object.entries(expectedFields).filter(([key, value]) => JSON.stringify(binaryFields[key]) === JSON.stringify(value)).length,
    };
    const rawExtracted = extractLocalText({ text: darkChat.rawText, schema: { schema_version: 1, definitions_version: 'synthetic-chat-v1', definitions }, document: { revision: 0, fields: {} }, inputRevision: 1 });
    const rawFields = Object.fromEntries(rawExtracted.candidates.flatMap(candidate => Object.entries(candidate.fields).map(([key, field]) => [key, field.value])));
    report.darkChat.unprocessedBaseline = { actualText: darkChat.rawText, coreFieldsMatched: Object.entries(expectedFields).filter(([key, value]) => JSON.stringify(rawFields[key]) === JSON.stringify(value)).length };
    report.notes.push('The dark chat sample is synthetic. Its four-field result is a per-fixture check, not a population accuracy estimate.');
    const variants = await page.evaluate(async lines => {
        const { prepareOCRImage } = await import('/frontend/src/ocr/preprocess.js');
        const { inspectImage } = await import('/frontend/src/ocr/images.js');
        const { createLocalRecognizer } = await import('/frontend/src/ocr/recognizer.js');
        const output = [];
        for (const sample of [{ name: 'synthetic-dark-chat-low', scale: 0.5, dark: true }, { name: 'synthetic-light-chat', scale: 1, dark: false }]) {
            const canvas = document.createElement('canvas'); canvas.width = 1600 * sample.scale; canvas.height = 1200 * sample.scale;
            const ctx = canvas.getContext('2d'); ctx.scale(sample.scale, sample.scale);
            ctx.fillStyle = sample.dark ? '#101820' : '#ffffff'; ctx.fillRect(0, 0, 1600, 1200);
            ctx.fillStyle = sample.dark ? '#202d3b' : '#f4f4f4'; ctx.fillRect(28, 24, 1544, 950);
            ctx.fillStyle = sample.dark ? '#27384a' : '#e8eef3'; ctx.fillRect(28, 990, 1544, 182);
            ctx.font = '40px sans-serif'; ctx.textBaseline = 'top';
            lines.forEach((line, index) => { ctx.fillStyle = sample.dark ? (index === 5 ? '#85c9ed' : index >= 6 && index <= 10 ? '#c0cbd5' : '#f1f5f8') : '#17202b'; ctx.fillText(line, 58, 56 + index * 80); });
            const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
            const prepared = await prepareOCRImage(blob);
            const dimensions = inspectImage(await prepared.arrayBuffer(), prepared.type);
            const recognizer = createLocalRecognizer({ resourceRoot: '/web/static/ocr/tesseract-7.0.0' });
            const raw = createLocalRecognizer({ resourceRoot: '/web/static/ocr/tesseract-7.0.0', prepareImage: file => file });
            try { output.push({ name: sample.name, width: canvas.width, height: canvas.height, dimensions, transformed: prepared !== blob, text: await recognizer.recognize(blob, ['chi_sim', 'eng'], () => {}), rawText: await raw.recognize(blob, ['chi_sim', 'eng'], () => {}), png: canvas.toDataURL('image/png').split(',')[1] }); }
            finally { recognizer.terminate(); raw.terminate(); }
        }
        return output;
    }, chatLines);
    report.chatControls = [];
    for (const variant of variants) {
        await writeFile(path.join(fixtures, variant.name + '.png'), Buffer.from(variant.png, 'base64'));
        const extract = text => {
            const result = extractLocalText({ text, schema: { schema_version: 1, definitions_version: 'synthetic-chat-v1', definitions }, document: { revision: 0, fields: {} }, inputRevision: 1 });
            return Object.fromEntries(result.candidates.flatMap(candidate => Object.entries(candidate.fields).map(([key, field]) => [key, field.value])));
        };
        const fields = extract(variant.text); const before = extract(variant.rawText);
        const matches = values => Object.entries(expectedFields).filter(([key, value]) => JSON.stringify(values[key]) === JSON.stringify(value)).length;
        const dimensionsPreserved = variant.width === variant.dimensions.width && variant.height === variant.dimensions.height;
        report.chatControls.push({ name: variant.name, width: variant.width, height: variant.height, dimensionsPreserved, transformed: variant.transformed, beforeFieldsMatched: matches(before), afterFieldsMatched: matches(fields), fields, actualText: variant.text });
        if (!dimensionsPreserved || variant.transformed || variant.text !== variant.rawText || matches(fields) < matches(before)) throw new Error('OCR contrast preprocessing changed a protected light/low-resolution control');
    }
    report.missingResourcesFails = await page.evaluate(async () => {
        const { createLocalRecognizer } = await import('/frontend/src/ocr/recognizer.js');
        const recognizer = createLocalRecognizer({ resourceRoot: '/missing-ocr-resources' });
        try { const image = await (await fetch('/tests/e2e/fixtures/ocr/eng.png')).blob(); await recognizer.recognize(image, ['eng'], () => {}); return false; } catch { return true; } finally { recognizer.terminate(); }
    });
    if (!report.missingResourcesFails || report.externalRequests.length) throw new Error('OCR resource isolation failed');
    // An already-cached static runtime works offline; only public assets are cached.
    await context.setOffline(true);
    report.cachedOfflineText = await page.evaluate(async () => {
        const { createLocalRecognizer } = await import('/frontend/src/ocr/recognizer.js');
        const canvas = document.createElement('canvas'); canvas.width = 700; canvas.height = 150; const ctx = canvas.getContext('2d'); ctx.fillStyle = 'white'; ctx.fillRect(0, 0, 700, 150); ctx.fillStyle = 'black'; ctx.font = '48px sans-serif'; ctx.fillText('Star Atlas', 30, 85);
        const blob = await new Promise(resolve => canvas.toBlob(resolve)); const recognizer = createLocalRecognizer({ resourceRoot: '/web/static/ocr/tesseract-7.0.0' });
        try { return await recognizer.recognize(blob, ['eng'], () => {}); } finally { recognizer.terminate(); }
    });
    if (!report.cachedOfflineText.includes('Star Atlas')) throw new Error('Cached offline OCR failed');
    console.log(JSON.stringify(report, null, 2));
    if (!report.darkChat.passed) throw new Error('Synthetic dark chat recognition did not meet its field and boundary baseline');
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
