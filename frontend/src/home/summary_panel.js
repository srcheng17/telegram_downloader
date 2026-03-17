export function renderSummary(doc, summary) {
    if (!doc || !summary) {
        return;
    }
    const totalNode = doc.getElementById('summary-total');
    const activeNode = doc.getElementById('summary-active');
    const successNode = doc.getElementById('summary-success');
    const failedNode = doc.getElementById('summary-failed');

    if (totalNode) {
        totalNode.textContent = String(summary.total_tasks || 0);
    }
    if (activeNode) {
        activeNode.textContent = String(summary.active_tasks || 0);
    }
    if (successNode) {
        successNode.textContent = String(summary.success_tasks || 0);
    }
    if (failedNode) {
        failedNode.textContent = String((summary.failed_tasks || 0) + (summary.canceled_tasks || 0));
    }
}

export function syncSummaryCollapseMode(win, doc) {
    const summaryCollapsible = doc.getElementById('summary-collapsible-home');
    if (!summaryCollapsible || typeof win.matchMedia !== 'function') {
        return;
    }
    const isMobile = win.matchMedia('(max-width: 768px)').matches;
    summaryCollapsible.open = !isMobile;
}
