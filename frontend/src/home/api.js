export function createHomeApi(api) {
    return {
        postDownload(formPayload, options = {}) {
            return api.postForm('/download', formPayload, options);
        },
        getSummary(options = {}) {
            return api.getJson('/api/summary', { cache: 'no-store', ...options });
        },
        getMetadataHistory(options = {}) {
            return api.getMetadataHistory(20, options);
        },
    };
}

