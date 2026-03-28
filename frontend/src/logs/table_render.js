export { buildStatusBadgeModel, mapTaskToLogViewModel } from './view_model.js';

import { mapTaskToLogViewModel } from './view_model.js';

export function getTaskTypeLabel(log) {
    return mapTaskToLogViewModel(log).taskTypeLabel;
}

export function formatProgressValue(log) {
    return mapTaskToLogViewModel(log).progressText;
}

export function shouldShowRetryAction(log) {
    return mapTaskToLogViewModel(log).canRetry;
}
