export async function normalizeHeadResult(response) {
    if (!response) {
        return { ok: false, status: 0 };
    }
    if (response.ok || response.status === 405 || response.status === 501) {
        return { ok: true, status: response.status };
    }
    return { ok: false, status: response.status };
}
