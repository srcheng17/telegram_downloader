import { IMAGE_LIMITS, validateImage } from './images.js';

export const OCR_LANGUAGES = Object.freeze(['chi_sim', 'chi_tra', 'jpn', 'eng']);

export function createOCRQueue({ recognizerFactory, validate = validateImage, createURL = file => URL.createObjectURL(file), revokeURL = url => URL.revokeObjectURL(url), id = () => `image-${crypto.randomUUID()}`, timeoutMs = 120000 } = {}) {
    let images = []; let inputRevision = 0; let disposed = false; let generation = 0;
    let languages = ['chi_sim', 'eng']; let recognizer = null; let active = null;
    let pumping = false; let admissions = Promise.resolve();
    const listeners = new Set();
    const snapshot = () => ({ images: images.map(image => { const publicImage = { ...image }; delete publicImage.file; return structuredClone(publicImage); }), inputRevision, languages: [...languages] });
    const publish = (changed = false) => { if (changed) inputRevision++; if (!disposed) listeners.forEach(listener => listener(snapshot())); };
    const find = imageID => images.find(image => image.id === imageID);
    function cancelActive(imageID) { if (active && (!imageID || active.id === imageID)) active.cancel(); }
    async function pump() {
        if (pumping || disposed) return;
        pumping = true;
        try {
            while (!disposed) {
                const image = images.find(item => item.status === 'queued'); if (!image) break;
                image.status = 'running'; image.progress = 0; image.error = ''; const run = ++image.ocrGeneration; const page = generation;
                let cancel; let timer;
                const cancelled = new Promise((_, reject) => { cancel = () => reject(new Error('cancelled')); timer = setTimeout(() => reject(new Error('timeout')), timeoutMs); });
                active = { id: image.id, cancel }; publish();
                try {
                    recognizer ||= recognizerFactory();
                    const current = recognizer;
                    const text = await Promise.race([current.recognize(image.file, [...languages], progress => {
                        if (disposed || page !== generation || find(image.id) !== image || image.ocrGeneration !== run || image.status !== 'running') return;
                        image.progress = Number.isFinite(progress) ? Math.max(0, Math.min(1, progress)) : 0; publish();
                    }), cancelled]);
                    if (disposed || page !== generation || find(image.id) !== image || image.ocrGeneration !== run || image.status !== 'running') continue;
                    if (typeof text !== 'string' || new TextEncoder().encode(text).length > 65536) throw new Error('invalid_result');
                    image.rawText = text; if (!image.dirty) { image.text = text; image.textRevision++; }
                    image.status = 'done'; image.progress = 1; publish(true);
                } catch (error) {
                    // Await termination before admitting another recognizer, even when
                    // the worker's rejected/late recognition promise never settles.
                    const retired = recognizer; recognizer = null;
                    try { await retired?.terminate(); } catch { /* already terminated */ }
                    if (!disposed && page === generation && find(image.id) === image && image.ocrGeneration === run) {
                        image.status = error.message === 'cancelled' ? 'cancelled' : 'failed';
                        image.error = error.message === 'cancelled' ? '已取消，可重试。' : error.message === 'timeout' ? '识别超时，请裁剪图片或重试。' : '本地识别失败，请确认资源可用后重试。';
                        publish(true);
                    }
                } finally { clearTimeout(timer); active = null; }
            }
        } finally { pumping = false; }
    }
    function add(files) {
        const page = generation; const batch = Array.from(files); const rejected = [];
        const operation = admissions.then(async () => {
            for (const file of batch) {
                if (disposed || page !== generation) break;
                try {
                    if (images.length >= IMAGE_LIMITS.count) throw new Error('最多可添加 10 张图片。');
                    if (images.reduce((total, item) => total + item.size, 0) + file.size > IMAGE_LIMITS.totalBytes) throw new Error('本组图片总大小不能超过 50 MiB。');
                    const info = await validate(file);
                    if (disposed || page !== generation) break;
                    const image = { id: id(), file, size: file.size, previewURL: createURL(file), ...info, status: 'queued', progress: 0, ocrGeneration: 0, rawText: '', text: '', textRevision: 0, dirty: false, error: '' };
                    images.push(image); publish(true); void pump();
                } catch (error) { rejected.push(error.message || '图片未能载入。'); }
            }
            batch.length = 0;
            return rejected;
        });
        admissions = operation.catch(() => {}); return operation;
    }
    function merge({ acceptPartial = false } = {}) {
        const excluded = images.filter(image => image.status !== 'done');
        if (excluded.length && !acceptPartial) throw new Error('仍有图片未识别成功。请等待完成，或明确选择“仅使用已完成图片”。');
        let text = ''; const segments = []; const warnings = []; const seen = new Set();
        for (const image of images.filter(item => item.status === 'done')) {
            if (!image.text) continue;
            if (text) text += '\n\n'; const start = text.length; text += image.text;
            segments.push({ image_id: image.id, text_revision: image.textRevision, start, end: text.length });
            for (const line of image.text.split('\n').map(line => line.trim()).filter(Boolean)) {
                if (seen.has(line) && !warnings.includes('可能有重复或重叠文字；已完整保留，请核对。')) warnings.push('可能有重复或重叠文字；已完整保留，请核对。');
                seen.add(line);
            }
        }
        return { text, segments, excluded: excluded.map(image => image.id), warnings, inputRevision };
    }
    return {
        snapshot, add, merge, whenAdmitted: () => admissions,
        subscribe(listener) { listeners.add(listener); return () => listeners.delete(listener); },
        setLanguages(next) { if (!Array.isArray(next) || !next.length || new Set(next).size !== next.length || next.some(lang => !OCR_LANGUAGES.includes(lang))) throw new Error('请选择受支持的识别语言。'); languages = [...next]; publish(true); },
        edit(imageID, text) { const image = find(imageID); if (!image || disposed) return; if (new TextEncoder().encode(text).length > 65536) throw new Error('单图文字不能超过 64 KiB。'); image.text = text; image.dirty = true; image.textRevision++; publish(true); },
        acceptRaw(imageID) { const image = find(imageID); if (!image || disposed || image.status !== 'done') return; image.text = image.rawText; image.dirty = false; image.textRevision++; publish(true); },
        retry(imageID) { const image = find(imageID); if (!image || disposed || ['running', 'queued'].includes(image.status)) return; image.status = 'queued'; image.error = ''; publish(true); void pump(); },
        cancel(imageID) { const image = find(imageID); if (!image) return; if (image.status === 'queued') { image.status = 'cancelled'; publish(true); } else cancelActive(imageID); },
        cancelAll() { images.filter(image => image.status === 'queued').forEach(image => { image.status = 'cancelled'; }); cancelActive(); publish(true); },
        remove(imageID) { const image = find(imageID); if (!image) return; cancelActive(imageID); image.ocrGeneration++; images = images.filter(item => item !== image); revokeURL(image.previewURL); image.file = null; image.text = ''; image.rawText = ''; publish(true); },
        move(imageID, delta) { const index = images.findIndex(image => image.id === imageID); const next = index + delta; if (index < 0 || ![-1, 1].includes(delta) || next < 0 || next >= images.length) return; [images[index], images[next]] = [images[next], images[index]]; publish(true); },
        async dispose() { if (disposed) return; disposed = true; generation++; cancelActive(); listeners.clear(); for (const image of images) { revokeURL(image.previewURL); image.file = null; image.text = ''; image.rawText = ''; } images = []; const retired = recognizer; recognizer = null; try { await retired?.terminate(); } catch { /* already terminated */ } },
    };
}
