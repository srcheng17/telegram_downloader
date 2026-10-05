import test from 'node:test';
import assert from 'node:assert/strict';
import { inspectImage, validateImage, IMAGE_LIMITS } from '../ocr/images.js';

function png(width = 640, height = 480, animated = false) {
    const chunk = (name, data) => { const b = Buffer.alloc(data.length + 12); b.writeUInt32BE(data.length); b.write(name, 4); data.copy(b, 8); return b; };
    const dimensions = Buffer.alloc(13); dimensions.writeUInt32BE(width); dimensions.writeUInt32BE(height, 4); dimensions[8] = 8; dimensions[9] = 2;
    return Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]), chunk('IHDR', dimensions), ...(animated ? [chunk('acTL', Buffer.alloc(8))] : []), chunk('IDAT', Buffer.from([0])), chunk('IEND', Buffer.alloc(0))]);
}
const file = (data, type = 'image/png', name = 'sample.png') => Object.assign(new Blob([data], { type }), { name });
test('image headers reject spoofed, animated, malformed and oversized inputs before decode', async () => {
    let decoded = 0;
    const decode = async () => { decoded++; return { width: 1, height: 1, close() {} }; };
    for (const input of [file(png(), 'image/jpeg'), file(png(), 'image/png', 'sample.jpg'), file(png(9000, 1)), file(png(4096, 4096)), file(png(1, 1, true)), file(png().subarray(0, 29)), file('<svg/>', 'image/svg+xml')]) {
        await assert.rejects(validateImage(input, { decode }));
    }
    assert.equal(decoded, 0);
    assert.deepEqual(inspectImage(png(8192, 1), 'image/png', 'x.png'), { width: 8192, height: 1, type: 'image/png' });
    assert.equal(IMAGE_LIMITS.count, 10);
});
test('decoded dimensions must match headers, and bitmaps always close', async () => {
    let closed = 0;
    await assert.rejects(validateImage(file(png()), { decode: async () => ({ width: 640, height: 479, close: () => closed++ }) }));
    assert.equal(closed, 1);
    const result = await validateImage(file(png()), { decode: async () => ({ width: 640, height: 480, close: () => closed++ }) });
    assert.equal(result.width, 640); assert.equal(closed, 2);
});
test('WebP rejects animation chunks and invalid container bounds', () => {
    const webp = Buffer.alloc(30); webp.write('RIFF'); webp.writeUInt32LE(22, 4); webp.write('WEBPVP8X', 8); webp.writeUInt32LE(10, 16); webp[24] = 9; webp[27] = 9;
    assert.deepEqual(inspectImage(webp, 'image/webp', 'x.webp'), { width: 10, height: 10, type: 'image/webp' });
    webp[20] = 2;
    assert.throws(() => inspectImage(webp, 'image/webp', 'x.webp'));
});
