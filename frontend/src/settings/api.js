export function createSettingsApi(api) {
    return {
        getSettings() {
            return api.getJson('/v2/settings', { cache: 'no-store' });
        },
        saveSettings(payload) {
            return api.putJson('/v2/settings', payload, {
                headers: { Accept: 'application/json' },
            });
        },
    };
}

