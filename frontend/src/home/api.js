export function createHomeApi(api) {
    return {
        postDownload(formPayload) {
            return api.postForm('/download', formPayload);
        },
        getSummary(options = {}) {
            return api.getJson('/api/summary', { cache: 'no-store', ...options });
        },
        getMetadataHistory() {
            return api.getMetadataHistory(20);
        },
    };
}

