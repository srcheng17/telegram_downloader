import { createTasksApi } from '../shared/api/tasks_api.js';

export function createKomgaApi(win = window) {
    const api = createTasksApi((url, options) => win.fetch(url, options), { win });
    return {
        libraries: options => api.getJson('/api/komga/libraries', options),
        books({ libraryID, query = '', page = 0, size = 24 }, options) {
            const params = new URLSearchParams();
            params.set('library_id', libraryID);
            params.set('query', query);
            params.set('page', String(page));
            params.set('size', String(size));
            return api.getJson(`/api/komga/books?${params.toString()}`, options);
        },
        book: (id, options) => api.getJson(`/api/komga/books/${encodeURIComponent(id)}`, options),
        edit: (id, options) => api.getJson(`/api/komga/books/${encodeURIComponent(id)}/edit`, options),
        preview: (id, input, options) => api.postJson(`/api/komga/books/${encodeURIComponent(id)}/preview`, input, options),
        save: (id, input, options) => api.postJson(`/api/komga/books/${encodeURIComponent(id)}/save`, input, options),
        operation: (id, options) => api.getJson(`/api/komga/edits/${encodeURIComponent(id)}`, options),
        retrySync: (id, options) => api.postJson(`/api/komga/edits/${encodeURIComponent(id)}/sync`, {}, options),
        restore: (id, options) => api.postJson(`/api/komga/edits/${encodeURIComponent(id)}/restore`, {}, options),
    };
}
