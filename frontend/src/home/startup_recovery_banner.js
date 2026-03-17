import {
    buildStartupRecoveryMessage,
    getStartupRecoveryDismissKey,
    parseStartupRecovery,
} from '../shared/startup_recovery.js';

export function createStartupRecoveryBannerController(win, doc, dismissKeyPrefix) {
    let currentDismissKey = '';

    function hide() {
        const banner = doc.getElementById('startup-recovery-banner-home');
        if (!banner) {
            return;
        }
        banner.classList.add('is-hidden');
        banner.setAttribute('aria-hidden', 'true');
    }

    function isDismissed(dismissKey) {
        if (!dismissKey) {
            return false;
        }
        try {
            return win.sessionStorage.getItem(dismissKey) === '1';
        } catch (error) {
            return false;
        }
    }

    function rememberDismissed() {
        if (!currentDismissKey) {
            return;
        }
        try {
            win.sessionStorage.setItem(currentDismissKey, '1');
        } catch (error) {
            // Ignore session storage failures.
        }
    }

    function render(summary) {
        const banner = doc.getElementById('startup-recovery-banner-home');
        const messageNode = doc.getElementById('startup-recovery-text-home');
        if (!banner || !messageNode) {
            return;
        }

        const recovery = parseStartupRecovery(summary);
        if (!recovery) {
            currentDismissKey = '';
            hide();
            return;
        }

        const dismissKey = getStartupRecoveryDismissKey(dismissKeyPrefix, recovery.signature);
        currentDismissKey = dismissKey;
        if (isDismissed(dismissKey)) {
            hide();
            return;
        }

        messageNode.textContent = buildStartupRecoveryMessage(recovery);
        banner.classList.remove('is-hidden');
        banner.setAttribute('aria-hidden', 'false');
    }

    function dismiss() {
        rememberDismissed();
        hide();
    }

    return {
        dismiss,
        hide,
        render,
    };
}
