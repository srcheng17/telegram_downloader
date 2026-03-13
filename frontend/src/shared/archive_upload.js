function normalizeText(value) {
    if (value === null || value === undefined) {
        return '';
    }
    return String(value).trim();
}

function normalizeCount(value) {
    const parsed = Number(value);
    if (!Number.isFinite(parsed) || parsed < 0) {
        return 0;
    }
    return Math.round(parsed);
}

export function normalizeUploadSnapshot(snapshot) {
    const raw = snapshot && typeof snapshot === 'object' ? snapshot : {};

    return {
        status: normalizeText(raw.status) || 'idle',
        loadedBytes: normalizeCount(raw.loadedBytes),
        totalBytes: normalizeCount(raw.totalBytes),
        fileName: normalizeText(raw.fileName),
        errorMessage: normalizeText(raw.errorMessage),
        canCancel: Boolean(raw.canCancel),
    };
}
