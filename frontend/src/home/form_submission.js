export function basePayloadForDuplicateRetry(formPayload) {
    const payload = { ...(formPayload || {}) };
    delete payload.force;
    return payload;
}

export function buildDuplicateActions(payload, basePayload) {
    return {
        basePayload: basePayloadForDuplicateRetry(basePayload),
        downloadUrl: payload && payload.download_url ? String(payload.download_url).trim() : '',
        downloadLabel: '立即下载',
        logsUrl: payload && payload.logs_url ? String(payload.logs_url).trim() : '/logs',
    };
}
