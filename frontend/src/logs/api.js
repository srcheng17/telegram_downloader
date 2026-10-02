export function createLogsApi(api) {
    return {
        getLogs(url, options = {}) {
            return api.getJson(url, { cache: 'no-store', ...options });
        },
        getSettingsMode(options = {}) {
            return api.getJson('/v2/settings', { cache: 'no-store', ...options });
        },
        head(url, options = {}) {
            return api.head(url, { cache: 'no-store', ...options });
        },
    };
}

