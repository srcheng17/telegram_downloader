import { normalizeUploadSnapshot } from '../shared/archive_upload.js';

function normalizeProgressSnapshot(file, loadedBytes, totalBytes) {
    return normalizeUploadSnapshot({
        status: 'uploading',
        loadedBytes,
        totalBytes,
        fileName: file && file.name ? file.name : '',
        errorMessage: '',
        canCancel: true,
    });
}

export function buildUploadInitPayload(file, metadata = {}) {
    return {
        ...(metadata || {}),
        file_name: file && file.name ? String(file.name) : '',
        file_size: file && Number.isFinite(Number(file.size)) ? Number(file.size) : 0,
    };
}

export function uploadArchiveSource({
    uploadUrl,
    uploadToken,
    file,
    onProgress,
    signal,
    createXHR = () => new XMLHttpRequest(),
}) {
    return new Promise((resolve, reject) => {
        if (signal && signal.aborted) {
            reject(new DOMException('上传已取消。', 'AbortError'));
            return;
        }
        const xhr = createXHR();
        const abort = () => xhr.abort();
        const finish = (callback, value) => {
            if (signal) signal.removeEventListener('abort', abort);
            callback(value);
        };
        xhr.upload.addEventListener('progress', (event) => {
            if (typeof onProgress !== 'function' || (signal && signal.aborted)) return;
            const totalBytes = event && event.lengthComputable ? event.total : file.size;
            onProgress(normalizeProgressSnapshot(file, event.loaded, totalBytes));
        });
        xhr.addEventListener('load', () => {
            let payload = null;
            try {
                payload = JSON.parse(xhr.responseText || 'null');
            } catch {
                // Proxies can return HTML errors instead of the API's JSON body.
            }
            if (xhr.status >= 200 && xhr.status < 300 && payload && payload.ok === true) {
                finish(resolve, payload);
                return;
            }
            finish(reject, new Error((payload && payload.message) || `上传失败（${xhr.status}）。`));
        });
        xhr.addEventListener('error', () => finish(reject, new Error('网络异常，上传失败。')));
        xhr.addEventListener('abort', () => finish(reject, new DOMException('上传已取消。', 'AbortError')));
        xhr.addEventListener('timeout', () => finish(reject, new Error('上传超时，请稍后重试。')));
        xhr.timeout = 10 * 60 * 1000;
        try {
            xhr.open('PUT', String(uploadUrl || '').trim());
            xhr.setRequestHeader('Accept', 'application/json');
            if (uploadToken) xhr.setRequestHeader('X-Upload-Token', String(uploadToken).trim());
            if (signal) signal.addEventListener('abort', abort, { once: true });
            xhr.send(file);
        } catch (error) {
            finish(reject, error);
        }
    });
}

export async function submitArchive({
    api,
    file,
    metadata,
    onProgress,
    onInit,
    signal,
    createXHR,
}) {
    const initPayload = buildUploadInitPayload(file, metadata);
    const initResult = await api.postJson('/api/tasks/upload/init', initPayload, { signal });
    if (!initResult.response || !initResult.response.ok || !initResult.payload || initResult.payload.ok !== true) {
        throw new Error((initResult.payload && initResult.payload.message) || '上传初始化失败。');
    }
    if (typeof onInit === 'function') {
        onInit(initResult.payload);
    }

    const uploadPayload = await uploadArchiveSource({
        uploadUrl: initResult.payload.upload_url,
        uploadToken: initResult.payload.upload_token,
        file,
        onProgress,
        signal,
        createXHR,
    });

    return {
        initPayload: initResult.payload,
        uploadPayload,
    };
}
