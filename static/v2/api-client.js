(function (global) {
    'use strict';

    class V2ApiError extends Error {
        constructor(message, status, payload) {
            super(message);
            this.name = 'V2ApiError';
            this.status = Number(status) || 0;
            this.payload = payload || null;
        }
    }

    function normalizeTaskID(taskID) {
        return String(taskID || '').trim();
    }

    function safeJSONParse(rawText) {
        if (!rawText) {
            return null;
        }
        try {
            return JSON.parse(rawText);
        } catch (error) {
            return null;
        }
    }

    function extractErrorMessage(payload, fallback) {
        if (!payload || typeof payload !== 'object') {
            return fallback;
        }
        const message = String(payload.error || payload.message || '').trim();
        if (message) {
            return message;
        }
        return fallback;
    }

    function buildURL(path, query) {
        const url = new URL(path, global.location.origin);
        if (!query || typeof query !== 'object') {
            return url.toString();
        }
        Object.entries(query).forEach(([key, value]) => {
            if (value === null || value === undefined) {
                return;
            }
            const text = String(value).trim();
            if (!text) {
                return;
            }
            url.searchParams.set(key, text);
        });
        return url.toString();
    }

    async function requestJSON(path, options) {
        const resolvedOptions = options && typeof options === 'object' ? options : {};
        const method = String(resolvedOptions.method || 'GET').toUpperCase();
        const query = resolvedOptions.query || null;
        const headers = Object.assign(
            {
                Accept: 'application/json',
            },
            resolvedOptions.headers || {},
        );

        const fetchOptions = {
            method,
            headers,
        };
        if (resolvedOptions.body !== undefined) {
            fetchOptions.body = JSON.stringify(resolvedOptions.body);
            fetchOptions.headers['Content-Type'] = 'application/json';
        }

        let response;
        try {
            response = await global.fetch(buildURL(path, query), fetchOptions);
        } catch (error) {
            throw new V2ApiError('network error', 0, null);
        }

        const responseText = await response.text();
        const payload = safeJSONParse(responseText);
        if (!response.ok) {
            const fallback = `request failed (${response.status})`;
            throw new V2ApiError(
                extractErrorMessage(payload, fallback),
                response.status,
                payload,
            );
        }

        return payload;
    }

    function buildArtifactURL(taskID) {
        const normalizedTaskID = normalizeTaskID(taskID);
        return `/v2/tasks/${encodeURIComponent(normalizedTaskID)}/artifact`;
    }

    const api = {
        V2ApiError,
        requestJSON,

        async createTask(input) {
            const payload = input && typeof input === 'object' ? input : {};
            return requestJSON('/v2/tasks', {
                method: 'POST',
                body: {
                    url: String(payload.url || '').trim(),
                },
            });
        },

        async listTasks(input) {
            const payload = input && typeof input === 'object' ? input : {};
            return requestJSON('/v2/tasks', {
                query: {
                    status: payload.status,
                    q: payload.q,
                    page: payload.page,
                    per_page: payload.per_page,
                },
            });
        },

        async getTask(taskID) {
            const normalizedTaskID = normalizeTaskID(taskID);
            return requestJSON(`/v2/tasks/${encodeURIComponent(normalizedTaskID)}`);
        },

        async cancelTask(taskID) {
            const normalizedTaskID = normalizeTaskID(taskID);
            return requestJSON(`/v2/tasks/${encodeURIComponent(normalizedTaskID)}/cancel`, {
                method: 'POST',
            });
        },

        async downloadArtifact(taskID, options) {
            const resolvedOptions = options && typeof options === 'object' ? options : {};
            const artifactURL = buildArtifactURL(taskID);
            if (resolvedOptions.redirect === false) {
                return artifactURL;
            }
            global.location.href = artifactURL;
            return artifactURL;
        },
    };

    global.TelegraphV2API = api;
})(window);
