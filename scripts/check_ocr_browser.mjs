// Dedicated real-WASM acceptance check; fixture text is entirely synthetic.
import { chromium } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

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
        await writeFile(path.join(fixtures, sample.language + '.png'), Buffer.from(result.png, 'base64'));
        if (!result.text.trim()) throw new Error('Empty real OCR output: ' + sample.language);
        const normalize = text => text.replace(/\s/gu, ''); const expected = normalize(sample.lines.join('\n')); const actual = normalize(result.text);
        let row = Array.from({ length: actual.length + 1 }, (_, i) => i);
        for (let i = 1; i <= expected.length; i++) { const next = [i]; for (let j = 1; j <= actual.length; j++) next[j] = Math.min(next[j - 1] + 1, row[j] + 1, row[j - 1] + (expected[i - 1] === actual[j - 1] ? 0 : 1)); row = next; }
        report.results.push({ language: sample.language, expected: sample.lines.join('\n'), actual: result.text, elapsedMs: result.elapsedMs, characterEditsIgnoringWhitespace: row[actual.length], expectedCharactersIgnoringWhitespace: expected.length });
    }
    report.missingResourcesFails = await page.evaluate(async () => {
        const { createLocalRecognizer } = await import('/frontend/src/ocr/recognizer.js');
        const recognizer = createLocalRecognizer({ resourceRoot: '/missing-ocr-resources' });
        try { await recognizer.recognize(new Blob(['bad']), ['eng'], () => {}); return false; } catch { return true; } finally { recognizer.terminate(); }
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
} finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
