export function createSettingsApi(api) {
    return {
        getSettings(options = {}) {
            return api.getJson('/v2/settings', { cache: 'no-store', ...options });
        },
        saveSettings(payload, options = {}) {
            return api.putJson('/v2/settings', payload, {
                ...options,
                headers: { Accept: 'application/json', ...(options.headers || {}) },
            });
        },
    };
}

