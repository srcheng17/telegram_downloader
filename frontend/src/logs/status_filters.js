import { buildStatusCatalog } from '../shared/models/status_catalog.js';

export function applyStatusCatalog(rawCatalog) {
    return buildStatusCatalog(rawCatalog);
}
