export const IMAGE_LIMITS = Object.freeze({ count: 10, bytes: 10 * 1024 * 1024, totalBytes: 50 * 1024 * 1024, pixels: 12000000, edge: 8192 });
const invalid = () => { throw new Error('图片格式或尺寸无效；仅支持静态 PNG、JPEG、WebP。'); };
function dimensions(width, height, type) {
    if (!Number.isInteger(width) || !Number.isInteger(height) || width < 1 || height < 1) invalid();
    if (width > IMAGE_LIMITS.edge || height > IMAGE_LIMITS.edge || width * height > IMAGE_LIMITS.pixels) throw new Error('图片超过 1200 万像素或单边 8192 像素，请裁剪后重试。');
    return { width, height, type };
}

// The entire compressed file is capped at 10 MiB. Scan container headers before
// invoking any pixel decoder, including chunks after image data (APNG/WebP).
export function inspectImage(input, mime, name = '') {
    const b = new Uint8Array(input.buffer || input, input.byteOffset || 0, input.byteLength);
    if (b.length > IMAGE_LIMITS.bytes || b.length < 12) invalid();
    const v = new DataView(b.buffer, b.byteOffset, b.byteLength);
    const str = (start, count) => String.fromCharCode(...b.subarray(start, start + count));
    let type; let width; let height;
    if (b[0] === 137 && str(1, 7) === 'PNG\r\n\x1a\n') {
        type = 'image/png'; let ended = false; let data = false;
        for (let p = 8; p < b.length;) {
            if (p + 12 > b.length) invalid();
            const size = v.getUint32(p); const kind = str(p + 4, 4); const end = p + size + 12;
            if (end > b.length || ['acTL', 'fcTL', 'fdAT'].includes(kind)) invalid();
            if (p === 8 && (kind !== 'IHDR' || size !== 13)) invalid();
            if (kind === 'IHDR') { if (p !== 8 || size !== 13) invalid(); width = v.getUint32(p + 8); height = v.getUint32(p + 12); }
            if (kind === 'IDAT') data = true;
            if (kind === 'IEND') { if (size || end !== b.length || !data) invalid(); ended = true; }
            p = end;
        }
        if (!ended) invalid();
    } else if (b[0] === 255 && b[1] === 216) {
        type = 'image/jpeg'; let p = 2; let ended = false;
        while (p < b.length) {
            if (b[p++] !== 255) invalid();
            while (b[p] === 255) p++;
            const marker = b[p++];
            if (marker === 217) { ended = true; if (p !== b.length) invalid(); break; }
            if (marker === 216 || marker === 0 || p + 2 > b.length) invalid();
            const size = v.getUint16(p); if (size < 2 || p + size > b.length) invalid();
            if (marker === 226 && str(p + 2, 4) === 'MPF\0') invalid();
            if ([192, 193, 194].includes(marker)) { if (size < 8 || width) invalid(); height = v.getUint16(p + 3); width = v.getUint16(p + 5); }
            p += size;
            if (marker === 218) {
                // Entropy-coded segments contain escaped FF bytes and restart markers.
                while (p < b.length) {
                    if (b[p] !== 255) { p++; continue; }
                    let next = p + 1; while (b[next] === 255) next++;
                    if (b[next] === 0 || (b[next] >= 208 && b[next] <= 215)) { p = next + 1; continue; }
                    break;
                }
            }
        }
        if (!ended) invalid();
    } else if (str(0, 4) === 'RIFF' && str(8, 4) === 'WEBP') {
        type = 'image/webp'; if (v.getUint32(4, true) + 8 !== b.length) invalid();
        let images = 0;
        for (let p = 12; p < b.length;) {
            if (p + 8 > b.length) invalid();
            const kind = str(p, 4); const size = v.getUint32(p + 4, true); const q = p + 8; const end = q + size + (size % 2);
            if (end > b.length || ['ANIM', 'ANMF'].includes(kind)) invalid();
            if (kind === 'VP8X') { if (size !== 10 || b[q] & 2) invalid(); width = 1 + b[q + 4] + b[q + 5] * 256 + b[q + 6] * 65536; height = 1 + b[q + 7] + b[q + 8] * 256 + b[q + 9] * 65536; }
            if (kind === 'VP8 ') { if (++images > 1 || size < 10 || str(q + 3, 3) !== '\x9d\x01\x2a') invalid(); const w = v.getUint16(q + 6, true) & 16383; const h = v.getUint16(q + 8, true) & 16383; if (width && (w !== width || h !== height)) invalid(); width = w; height = h; }
            if (kind === 'VP8L') { if (++images > 1 || size < 5 || b[q] !== 47) invalid(); const bits = v.getUint32(q + 1, true); const w = (bits & 16383) + 1; const h = ((bits >>> 14) & 16383) + 1; if (width && (w !== width || h !== height)) invalid(); width = w; height = h; }
            p = end;
        }
    } else invalid();
    const extension = /\.([^.]+)$/u.exec(name)?.[1]?.toLowerCase();
    const extensions = { 'image/png': ['png'], 'image/jpeg': ['jpg', 'jpeg'], 'image/webp': ['webp'] };
    if (mime !== type || (extension && !extensions[type].includes(extension))) invalid();
    return dimensions(width, height, type);
}

export async function validateImage(file, { decode = blob => createImageBitmap(blob, { imageOrientation: 'none' }) } = {}) {
    if (!file || file.size < 1 || file.size > IMAGE_LIMITS.bytes) throw new Error('单张图片不能超过 10 MiB。');
    const info = inspectImage(await file.arrayBuffer(), file.type, file.name);
    let bitmap;
    try {
        bitmap = await decode(file);
        dimensions(bitmap.width, bitmap.height, info.type);
        if (bitmap.width !== info.width || bitmap.height !== info.height) invalid();
    } catch { throw new Error('图片无法解码或实际尺寸与文件头不符。'); }
    finally { bitmap?.close(); }
    return info;
}
