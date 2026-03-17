export function normalizeStatusCode(status) {
    return String(status || '').trim().toUpperCase();
}

export function fallbackStatusLabel(statusCode) {
    return normalizeStatusCode(statusCode).replace(/_/g, ' ') || '未知状态';
}

export function getStatusMeta(catalog, status) {
    const statusCode = normalizeStatusCode(status);
    if (!statusCode || !catalog || typeof catalog !== 'object') {
        return null;
    }
    for (const [rawStatus, rawMeta] of Object.entries(catalog)) {
        if (normalizeStatusCode(rawStatus) === statusCode) {
            return rawMeta && typeof rawMeta === 'object' ? rawMeta : null;
        }
    }
    return null;
}

export function buildStatusCatalog(rawCatalog, fallbackLabel = fallbackStatusLabel) {
    if (!rawCatalog || typeof rawCatalog !== 'object') {
        return {};
    }
    const nextCatalog = {};
    Object.entries(rawCatalog).forEach(([rawStatus, rawMeta]) => {
        const statusCode = normalizeStatusCode(rawStatus);
        if (!statusCode) {
            return;
        }
        const metadata = rawMeta && typeof rawMeta === 'object' ? rawMeta : {};
        nextCatalog[statusCode] = {
            label:
                typeof metadata.label === 'string' && metadata.label.trim()
                    ? metadata.label.trim()
                    : fallbackLabel(statusCode),
            can_cancel: Boolean(metadata.can_cancel),
            can_download: Boolean(metadata.can_download),
        };
    });
    return nextCatalog;
}
