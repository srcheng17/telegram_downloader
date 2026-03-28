import { normalizeStatusCode } from '../shared/models/status_catalog.js';

export function readLogsFiltersFromForm(doc) {
    const statusInput = doc.getElementById('status-filter');
    const queryInput = doc.getElementById('query-filter');
    return {
        status: statusInput ? normalizeStatusCode(statusInput.value) : '',
        q: queryInput ? String(queryInput.value || '').trim() : '',
    };
}

export function buildLogsUrl(filters, page, perPage) {
    const params = new URLSearchParams();
    params.set('page', String(page));
    params.set('per_page', String(perPage));
    if (filters && filters.status) {
        params.set('status', filters.status);
    }
    if (filters && filters.q) {
        params.set('q', filters.q);
    }
    return `/api/logs?${params.toString()}`;
}

