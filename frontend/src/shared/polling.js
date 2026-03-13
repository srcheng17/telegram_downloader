function toNonNegativeInteger(value, fallback) {
    const parsed = Number(value);
    if (!Number.isFinite(parsed) || parsed < 0) {
        return fallback;
    }
    return Math.round(parsed);
}

export function resolvePollDelay(options = {}) {
    const hidden = Boolean(options.hidden);
    const failures = toNonNegativeInteger(options.failures, 0);
    const active = Boolean(options.active);
    const activePollMs = toNonNegativeInteger(options.activePollMs, 2000);
    const idlePollMs = toNonNegativeInteger(options.idlePollMs, 8000);
    const hiddenPollMs = toNonNegativeInteger(options.hiddenPollMs, 30000);
    const maxBackoffMs = toNonNegativeInteger(options.maxBackoffMs, 60000);
    const baseDelayMs = active ? activePollMs : idlePollMs;
    const failureBackoffMs = failures > 0 ? Math.min(maxBackoffMs, activePollMs * 2 ** (failures - 1)) : 0;
    const visibilityFloorMs = hidden ? hiddenPollMs : 0;

    return Math.max(baseDelayMs, visibilityFloorMs, failureBackoffMs);
}
