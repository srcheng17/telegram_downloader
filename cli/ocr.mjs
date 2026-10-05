import { constants } from 'node:fs';
import { open } from 'node:fs/promises';
import { extname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';
import { createWorker } from 'tesseract.js';
import { IMAGE_LIMITS, inspectImage } from '../frontend/src/ocr/images.js';
import { OCR_LANGUAGES } from '../frontend/src/ocr/queue.js';
import { CliError, EXIT } from './errors.mjs';

const OCR_ROOT = resolve(fileURLToPath(new URL('../web/static/ocr/tesseract-7.0.0/', import.meta.url)));
const MIME = { '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp' };

function imageError(message = '图片格式或尺寸无效；仅支持静态 PNG、JPEG、WebP。') {
    return new CliError('invalid_image', message);
}

export function validateLanguages(value) {
    if (!Array.isArray(value) || value.length < 1 || new Set(value).size !== value.length || value.some((item) => !OCR_LANGUAGES.includes(item))) {
        throw new CliError('invalid_input', '识别语言只能从 chi_sim、chi_tra、jpn、eng 中选择。');
    }
    return [...value];
}

export function validateImageBytes(bytes, name, mime = MIME[extname(name).toLowerCase()]) {
    if (!Buffer.isBuffer(bytes) || bytes.length < 1 || bytes.length > IMAGE_LIMITS.bytes || !mime) throw imageError();
    try { return inspectImage(bytes, mime, name); }
    catch { throw imageError(); }
}

export async function readImageFile(path) {
    let file;
    try {
        file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
        const stat = await file.stat();
        if (!stat.isFile() || stat.size < 1 || stat.size > IMAGE_LIMITS.bytes) throw imageError('单张图片不能超过 10 MiB。');
        const bytes = Buffer.alloc(stat.size);
        let offset = 0;
        while (offset < bytes.length) {
            const result = await file.read(bytes, offset, bytes.length - offset, null);
            if (!result.bytesRead) throw imageError('截图读取期间发生变化，请重试。');
            offset += result.bytesRead;
        }
        const extra = Buffer.alloc(1);
        if ((await file.read(extra, 0, 1, null)).bytesRead !== 0) throw imageError('截图读取期间发生变化，请重试。');
        return { bytes, info: validateImageBytes(bytes, path) };
    } catch (error) {
        if (error instanceof CliError) throw error;
        throw imageError('无法读取本地截图。');
    } finally { await file?.close(); }
}

// AppKit reads the clipboard in this process' child, bounds the original data
// before ImageIO decoding, and writes only the resulting PNG to stdout. The
// screenshot is never written to a temporary file or passed in argv.
const SWIFT_CLIPBOARD = `import AppKit
import ImageIO
import Foundation
let board = NSPasteboard.general
let types: [NSPasteboard.PasteboardType] = [.png, .init("public.jpeg"), .tiff, .init("org.webmproject.webp")]
guard let kind = types.first(where: { board.data(forType: $0) != nil }), let raw = board.data(forType: kind), raw.count > 0, raw.count <= 52428800 else { exit(2) }
guard let source = CGImageSourceCreateWithData(raw as CFData, [kCGImageSourceShouldCache: false] as CFDictionary), CGImageSourceGetCount(source) == 1,
      let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
      let w = properties[kCGImagePropertyPixelWidth] as? Int, let h = properties[kCGImagePropertyPixelHeight] as? Int,
      w > 0, h > 0, w <= 8192, h <= 8192, w * h <= 12000000,
      let image = CGImageSourceCreateImageAtIndex(source, 0, nil), image.width == w, image.height == h else { exit(3) }
let rep = NSBitmapImageRep(cgImage: image)
guard let png = rep.representation(using: .png, properties: [:]), png.count > 0, png.count <= 10485760 else { exit(4) }
FileHandle.standardOutput.write(png)
`;

export async function readClipboardImage({ platform = process.platform, spawnProcess = spawn } = {}) {
    if (platform !== 'darwin') throw new CliError('unsupported_platform', '此系统不支持剪贴板截图，请使用 --image 文件。', EXIT.unavailable);
    return new Promise((resolvePromise, reject) => {
        const child = spawnProcess('/usr/bin/swift', ['-e', SWIFT_CLIPBOARD], { stdio: ['ignore', 'pipe', 'ignore'] });
        const chunks = []; let size = 0; let rejected = false;
        const fail = () => { if (!rejected) { rejected = true; child.kill(); reject(imageError('剪贴板中没有可用的静态截图，或图片超过限制。')); } };
        child.stdout.on('data', (chunk) => { size += chunk.length; if (size > IMAGE_LIMITS.bytes) fail(); else chunks.push(chunk); });
        child.once('error', fail);
        child.once('exit', (code) => {
            if (rejected) return;
            if (code !== 0) return fail();
            try { const bytes = Buffer.concat(chunks); resolvePromise({ bytes, info: validateImageBytes(bytes, 'clipboard.png') }); }
            catch { fail(); }
        });
    });
}

export function createNodeRecognizer({ workerFactory = createWorker, root = OCR_ROOT, timeoutMs = 120_000 } = {}) {
    let worker = null; let selection = ''; let rejectActive = null; let generation = 0;
    async function close() {
        generation++;
        const reject = rejectActive; rejectActive = null; reject?.(new Error('cancelled'));
        const old = worker; worker = null; selection = ''; await old?.terminate();
    }
    return {
        async recognize(bytes, info, languages) {
            validateLanguages(languages);
            const next = languages.join('+');
            if (worker && next !== selection) await close();
            const ownGeneration = generation;
            const cancelled = new Promise((_, reject) => { rejectActive = reject; });
            let timer;
            try {
                if (!worker) {
                    const creation = workerFactory(languages, 1, {
                        corePath: resolve(root, 'core'), langPath: resolve(root, 'lang'),
                        cacheMethod: 'none', gzip: true, logging: false,
                    });
                    void creation.then((created) => { if (generation !== ownGeneration) void created.terminate(); }, () => {});
                    worker = await Promise.race([creation, cancelled]);
                    if (generation !== ownGeneration) throw new Error('cancelled');
                    selection = next;
                }
                const result = await Promise.race([
                    worker.recognize(bytes, {}, { text: true, imageColor: true }),
                    new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('timeout')), timeoutMs); }),
                    cancelled,
                ]);
                const text = result?.data?.text;
                const color = result?.data?.imageColor;
                if (typeof text !== 'string' || Buffer.byteLength(text) > 65536 || typeof color !== 'string' || !color.startsWith('data:image/png;base64,')) throw new Error('invalid result');
                // Tesseract serializes its decoded pixels as PNG. Checking its IHDR
                // compares the decoder's dimensions with the preflight container.
                const header = Buffer.from(color.slice('data:image/png;base64,'.length, 'data:image/png;base64,'.length + 48), 'base64');
                if (header.length < 24 || header.subarray(0, 8).toString('hex') !== '89504e470d0a1a0a' || header.readUInt32BE(16) !== info.width || header.readUInt32BE(20) !== info.height) throw new Error('decoded dimensions mismatch');
                return text;
            } catch {
                await close().catch(() => {});
                throw new CliError('ocr_failed', '本地 OCR 未完成；请核对图片及离线识别资源。', EXIT.unavailable);
            } finally { clearTimeout(timer); if (generation === ownGeneration) rejectActive = null; }
        },
        close,
    };
}
