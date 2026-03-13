export function createSettingsModule() {
    return {
        mount() {},
        unmount() {},
    };
}

if (!window.TelegraphDownloaderSettings) {
    window.TelegraphDownloaderSettings = createSettingsModule();
}
