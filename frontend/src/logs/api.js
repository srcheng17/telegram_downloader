export function createLogsApi(api) {
    return {
        getLogs(url, options = {}) {
            return api.getJson(url, { cache: 'no-store', ...options });
        },
        getSettingsMode() {
            return api.getJson('/v2/settings', { cache: 'no-store' });
        },
        head(url) {
            return api.head(url, { cache: 'no-store' });
        },
    };
}

