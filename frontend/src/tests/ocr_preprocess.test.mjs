import test from 'node:test';
import assert from 'node:assert/strict';
import { normalizeDarkScreenshot, prepareOCRImage } from '../ocr/preprocess.js';

function pixels(background, foreground, count = 1000) {
    const data = new Uint8ClampedArray(count * 4);
    for (let i = 0; i < count; i++) data.set([...(i % 20 === 0 ? foreground : background), 255], i * 4);
    return data;
}

test('obvious dark background becomes black text on white without changing source pixels', () => {
    const input = pixels([32, 45, 59], [241, 245, 248]);
    const before = input.slice();
    const result = normalizeDarkScreenshot(input);
    assert.equal(result.changed, true);
    assert.deepEqual(input, before);
    assert.deepEqual(Array.from(result.pixels.slice(0, 8)), [0, 0, 0, 255, 255, 255, 255, 255]);
});

test('white, transparent, blank dark and mixed midtone images retain their original pixels', () => {
    const transparent = pixels([32, 45, 59], [241, 245, 248]); transparent[3] = 0;
    for (const input of [pixels([255, 255, 255], [0, 0, 0]), transparent, pixels([32, 45, 59], [32, 45, 59]), pixels([125, 125, 125], [230, 230, 230])]) {
        const result = normalizeDarkScreenshot(input);
        assert.equal(result.changed, false);
        assert.equal(result.pixels, input);
    }
});

test('preprocessor checks compressed bounds before pixel decoding', async () => {
    let decoded = false;
    await assert.rejects(prepareOCRImage(new Blob(['invalid'], { type: 'image/png' }), { decode: async () => { decoded = true; } }));
    assert.equal(decoded, false);
});

test('decoded dimension mismatch closes bitmap and never draws', async () => {
    // The real checked-in tiny PNG supplies valid headers; the injected decoder lies.
    const { readFile } = await import('node:fs/promises');
    const bytes = await readFile(new URL('../../../tests/e2e/fixtures/ocr/eng.png', import.meta.url));
    let closed = false; let drawn = false;
    await assert.rejects(prepareOCRImage(new Blob([bytes], { type: 'image/png' }), {
        decode: async () => ({ width: 1, height: 1, close() { closed = true; } }),
        createCanvas: () => { drawn = true; },
    }), /尺寸/);
    assert.equal(closed, true); assert.equal(drawn, false);
});


test('cancelling during preprocessing cannot start a late worker', async () => {
    const { createLocalRecognizer } = await import('../ocr/recognizer.js');
    let release; let workers = 0;
    const prepared = new Promise(resolve => { release = resolve; });
    const recognizer = createLocalRecognizer({ origin: 'https://local.test', WorkerClass: class { constructor() { workers++; } }, prepareImage: () => prepared });
    const pending = recognizer.recognize(new Blob(['synthetic']), ['eng'], () => {});
    recognizer.terminate(); release(new Blob(['synthetic']));
    await assert.rejects(pending, /已取消/);
    assert.equal(workers, 0);
});


test('small screenshots retain their original antialiasing and release decoded bitmap', async () => {
    const { readFile } = await import('node:fs/promises');
    const { inspectImage } = await import('../ocr/images.js');
    const bytes = await readFile(new URL('../../../tests/e2e/fixtures/ocr/eng.png', import.meta.url));
    const info = inspectImage(bytes, 'image/png');
    const file = new Blob([bytes], { type: 'image/png' }); let closed = false;
    const result = await prepareOCRImage(file, {
        decode: async () => ({ ...info, close() { closed = true; } }),
        createCanvas: () => { throw new Error('must not render small inputs'); },
    });
    assert.equal(result, file); assert.equal(closed, true);
});
