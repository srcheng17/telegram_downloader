export { buildStatusBadgeModel, mapTaskToLogViewModel } from './view_model.js';

import { buildStatusBadgeModel, mapTaskToLogViewModel } from './view_model.js';

export function getTaskTypeLabel(log) {
    return mapTaskToLogViewModel(log).taskTypeLabel;
}

export function formatProgressValue(log) {
    return mapTaskToLogViewModel(log).progressText;
}

export function shouldShowRetryAction(log) {
    return mapTaskToLogViewModel(log).canRetry;
}

export function createStatusBadgeElement(doc, view, statusCatalog) {
    const resolvedView = view && typeof view === 'object' ? view : {};
    const model = buildStatusBadgeModel(resolvedView.status, statusCatalog);
    const statusLabel = String(resolvedView.statusLabel || model.label || '').trim();
    const badge = doc.createElement('span');
    badge.className = model.className;
    badge.textContent = statusLabel;
    badge.title = model.statusCode;
    badge.setAttribute('data-task-status-label', statusLabel);
    badge.setAttribute('data-task-status-code', model.statusCode);
    return badge;
}


export function createStatusCellElement(doc, view, statusCatalog) {
    const cell = doc.createElement('td');
    const value = doc.createElement('div');
    value.className = 'log-cell-value';
    value.appendChild(createStatusBadgeElement(doc, view, statusCatalog));
    cell.appendChild(value);
    return cell;
}
