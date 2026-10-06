import { inspectImage, IMAGE_LIMITS } from './images.js';

// Restrict this treatment to high-contrast, predominantly dark screenshots.
// The 110 cut sits between the detected background (<=80) and light text (>=160).
// It is a deterministic contrast transform, not an attempt to repair glyphs.
export function normalizeDarkScreenshot(pixels) {
    if (!(pixels instanceof Uint8ClampedArray) || !pixels.length || pixels.length % 4) throw new Error('图片像素无效。');
    let dark = 0; let light = 0;
    const count = pixels.length / 4;
    for (let i = 0; i < pixels.length; i += 4) {
        if (pixels[i + 3] !== 255) return { changed: false, pixels };
        const gray = (pixels[i] + pixels[i + 1] + pixels[i + 2]) / 3;
        if (gray <= 80) dark++;
        if (gray >= 160) light++;
    }
    if (dark / count < 0.85 || light / count < 0.002 || light / count > 0.15) return { changed: false, pixels };
    const output = new Uint8ClampedArray(pixels.length);
    for (let i = 0; i < pixels.length; i += 4) {
        const gray = (pixels[i] + pixels[i + 1] + pixels[i + 2]) / 3;
        output[i] = output[i + 1] = output[i + 2] = gray > 110 ? 0 : 255;
        output[i + 3] = 255;
    }
    return { changed: true, pixels: output };
}

// Queue admission already validates files. Repeat the cheap container boundary
// here because the recognizer is also callable without a queue. The original
// File/preview stays untouched; only this temporary OCR input can be transformed.
export async function prepareOCRImage(file, {
    decode = blob => createImageBitmap(blob, { imageOrientation: 'none' }),
    createCanvas = () => document.createElement('canvas'),
} = {}) {
    if (!file || file.size < 1 || file.size > IMAGE_LIMITS.bytes) throw new Error('单张图片不能超过 10 MiB。');
    const info = inspectImage(await file.arrayBuffer(), file.type, file.name);
    let bitmap; let canvas;
    try {
        bitmap = await decode(file);
        if (bitmap.width !== info.width || bitmap.height !== info.height) throw new Error('图片实际尺寸与文件头不符。');
        // Hard thresholding removed useful antialiasing in the 800×600 control.
        // Keep small screenshots/crops on their original path; the measured
        // improvement is limited to screenshots with a >=1000px short edge.
        if (Math.min(info.width, info.height) < 1000) return file;
        canvas = createCanvas(); canvas.width = info.width; canvas.height = info.height;
        const context = canvas.getContext('2d', { willReadFrequently: true });
        if (!context) throw new Error('本地图片处理不可用。');
        context.drawImage(bitmap, 0, 0);
        bitmap.close(); bitmap = null;
        const image = context.getImageData(0, 0, info.width, info.height);
        const normalized = normalizeDarkScreenshot(image.data);
        if (!normalized.changed) return file;
        image.data.set(normalized.pixels); context.putImageData(image, 0, 0);
        const result = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
        if (!result) throw new Error('本地图片处理失败。');
        return result;
    } finally {
        bitmap?.close();
        if (canvas) { canvas.width = 0; canvas.height = 0; }
    }
}
