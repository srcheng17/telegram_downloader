import { OCR_LANGUAGES } from './queue.js';
import { prepareOCRImage } from './preprocess.js';

export const OCR_RESOURCE_ROOT = '/static/ocr/tesseract-7.0.0';

// Tesseract 7's createWorker resolves only after model initialization. Its pinned
// worker protocol lets us own the Worker immediately, so cancel/unmount also
// terminates a worker still downloading WASM/language resources.
export function createLocalRecognizer({ WorkerClass = Worker, resourceRoot = OCR_RESOURCE_ROOT, origin = location.origin, prepareImage = prepareOCRImage } = {}) {
    const base = new URL(resourceRoot, origin);
    if (base.origin !== origin || base.search || base.hash) throw new Error('识别资源必须来自本站。');
    let worker = null; let serial = 0; let initialized = ''; let dead = false; let progress = () => {};
    const pending = new Map();
    function terminate() {
        dead = true; worker?.terminate(); worker = null;
        for (const job of pending.values()) job.reject(new Error('本地识别已取消。'));
        pending.clear(); progress = () => {};
    }
    function start() {
        if (dead) throw new Error('本地识别已取消。');
        if (worker) return;
        worker = new WorkerClass(`${base.href}/worker.min.js`);
        worker.onmessage = ({ data }) => {
            if (dead || !data || data.workerId !== 'local-ocr') return;
            const job = pending.get(data.jobId); if (!job) return;
            if (data.status === 'progress') { if (data.data?.status === 'recognizing text') progress(data.data.progress); return; }
            pending.delete(data.jobId);
            if (data.status === 'resolve') job.resolve(data.data);
            else job.reject(new Error('本地识别资源或图片处理失败。'));
        };
        worker.onerror = event => { event.preventDefault?.(); terminate(); };
        worker.onmessageerror = () => terminate();
    }
    function request(action, payload) {
        start(); const jobId = `ocr-${++serial}`;
        return new Promise((resolve, reject) => { pending.set(jobId, { resolve, reject }); worker.postMessage({ workerId: 'local-ocr', jobId, action, payload }); });
    }
    return {
        async recognize(file, languages, onProgress) {
            if (!languages.length || languages.some(lang => !OCR_LANGUAGES.includes(lang))) throw new Error('识别语言无效。');
            const input = await prepareImage(file);
            if (dead) throw new Error('本地识别已取消。');
            progress = onProgress;
            if (!worker) await request('load', { options: { lstmOnly: true, corePath: `${base.href}/core`, logging: false } });
            const selection = languages.join('+');
            if (selection !== initialized) {
                await request('loadLanguage', { langs: languages, options: { langPath: `${base.href}/lang`, gzip: true, cacheMethod: 'none', lstmOnly: true } });
                await request('initialize', { langs: languages, oem: 1, config: {} }); initialized = selection;
            }
            const bytes = new Uint8Array(await input.arrayBuffer());
            const result = await request('recognize', { image: bytes, options: {}, output: { text: true, blocks: false, hocr: false, tsv: false } });
            if (typeof result?.text !== 'string') throw new Error('本地识别结果无效。');
            return result.text;
        },
        terminate,
    };
}
