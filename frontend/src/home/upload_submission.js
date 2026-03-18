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
    createXHR = () => new XMLHttpRequest(),
}) {
    return new Promise((resolve, reject) => {
        const xhr = createXHR();
        xhr.upload.addEventListener('progress', (event) => {
            if (typeof onProgress !== 'function') {
                return;
            }
            const totalBytes = event && event.lengthComputable ? event.total : file.size;
            onProgress(normalizeProgressSnapshot(file, event.loaded, totalBytes));
        });
        xhr.addEventListener('load', () => {
            const payload = JSON.parse(xhr.responseText || 'null');
            if (xhr.status >= 200 && xhr.status < 300) {
                resolve(payload);
                return;
            }
            reject(new Error((payload && payload.message) || `上传失败（${xhr.status}）`));
        });
        xhr.addEventListener('error', () => reject(new Error('上传失败。')));
        xhr.open('PUT', String(uploadUrl || '').trim());
        xhr.setRequestHeader('Accept', 'application/json');
        if (uploadToken) {
            xhr.setRequestHeader('X-Upload-Token', String(uploadToken).trim());
        }
        xhr.send(file);
    });
}

export async function submitArchive({
    api,
    file,
    metadata,
    onProgress,
    onInit,
    createXHR,
}) {
    const initPayload = buildUploadInitPayload(file, metadata);
    const initResult = await api.postJson('/api/tasks/upload/init', initPayload);
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
        createXHR,
    });

    return {
        initPayload: initResult.payload,
        uploadPayload,
    };
}
