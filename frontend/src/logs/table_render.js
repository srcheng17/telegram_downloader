import { fallbackStatusLabel, getStatusMeta, normalizeStatusCode } from '../shared/models/status_catalog.js';

export function buildStatusBadgeModel(status, statusCatalog) {
    const statusCode = normalizeStatusCode(status) || 'UNKNOWN';
    const statusMeta = getStatusMeta(statusCatalog, statusCode);
    return {
        statusCode,
        label: statusMeta ? statusMeta.label : fallbackStatusLabel(statusCode),
        className: `status-badge status-${statusCode.toLowerCase().replace(/[^a-z_]/g, '') || 'unknown'}`,
    };
}
