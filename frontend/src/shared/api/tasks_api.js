function toFormUrlEncoded(data) {
    return Object.keys(data || {})
        .map((key) => `${encodeURIComponent(key)}=${encodeURIComponent(data[key])}`)
        .join('&');
}

export function createTasksApi(fetchImpl = fetch) {
    async function requestJson(url, options = {}) {
        const response = await fetchImpl(url, options);
        const payload = await response.json().catch(() => null);
        return { response, payload };
    }

    return {
        requestJson,
        getJson(url, options = {}) {
            return requestJson(url, {
                method: 'GET',
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
            return fetchImpl(url, {
                method: 'HEAD',
                ...options,
            });
        },
        async createTask(payload) {
            const { payload: result } = await requestJson('/v2/tasks', {
                method: 'POST',
                headers: {
                    Accept: 'application/json',
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(payload),
            });
            return result;
        },
    };
}
