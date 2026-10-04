function toFormUrlEncoded(data) {
    return Object.keys(data || {})
        .map((key) => `${encodeURIComponent(key)}=${encodeURIComponent(data[key])}`)
        .join('&');
}

export function createTasksApi(fetchImpl = fetch, { win = globalThis.window } = {}) {
    function assertLocal(url) {
        const origin = win?.location?.origin;
        if (origin && new URL(url, origin).origin !== origin) throw new Error('不能向外部地址发送工作区请求。');
        if (!origin && (!String(url).startsWith('/') || String(url).startsWith('//'))) throw new Error('工作区请求必须使用本地地址。');
    }
    function unauthorized() {
        win?.__adminSession?.clear();
        win?.__onAdminUnauthorized?.();
    }
    async function csrfHeaders(url) {
        assertLocal(url);
        const session = win?.__adminSession;
        if (!session) return {};
        if (!session.getCSRFToken()) await session.refresh();
        if (!session.isAuthenticated()) {
            unauthorized();
            throw new Error('登录已失效，请重新登录。');
        }
        return { 'X-CSRF-Token': session.getCSRFToken() };
    }
    async function request(url, options = {}) {
        assertLocal(url);
        const method = String(options.method || 'GET').toUpperCase();
        const headers = { ...(options.headers || {}) };
        if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) Object.assign(headers, await csrfHeaders(url));
        if (options.signal?.aborted) throw new DOMException('请求已取消', 'AbortError');
        const response = await fetchImpl(url, { ...options, credentials: 'same-origin', cache: 'no-store', headers });
        if (response.status === 401) unauthorized();
        return response;
    }
    async function requestJson(url, options = {}) {
        const response = await request(url, options);
        const payload = await response.json().catch(() => null);
        return { response, payload };
    }

    return {
        requestJson,
        csrfHeaders,
        unauthorized,
        getJson(url, options = {}) {
            return requestJson(url, {
                method: 'GET',
                ...options,
            });
        },
        getMetadataHistory(limit = 20, options = {}) {
            const params = new URLSearchParams();
            params.set('limit', String(limit));
            return requestJson(`/api/metadata-history?${params.toString()}`, {
                method: 'GET',
                cache: 'no-store',
                ...options,
            });
        },
        postJson(url, payload, options = {}) {
            return requestJson(url, {
                method: 'POST',
                ...options,
                headers: {
                    'Content-Type': 'application/json',
                    ...(options.headers || {}),
                },
                body: JSON.stringify(payload),
            });
        },
        putJson(url, payload, options = {}) {
            return requestJson(url, {
                method: 'PUT',
                ...options,
                headers: {
                    'Content-Type': 'application/json',
                    ...(options.headers || {}),
                },
                body: JSON.stringify(payload),
            });
        },
        postForm(url, formPayload, options = {}) {
            return requestJson(url, {
                method: 'POST',
                ...options,
                headers: {
                    Accept: 'application/json',
                    'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8',
                    ...(options.headers || {}),
                },
                body: toFormUrlEncoded(formPayload),
            });
        },
        head(url, options = {}) {
            return request(url, {
                method: 'HEAD',
                ...options,
            });
        },
    };
}
