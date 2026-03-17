export function showErrorModal(doc, state, errorText, triggerElement) {
    const modal = doc.getElementById('error-modal');
    const content = doc.getElementById('error-modal-content');
    const closeButton = doc.getElementById('error-modal-close');
    if (!modal || !content || !closeButton) {
        return;
    }
    state.modalRestoreFocusEl = triggerElement || doc.activeElement;
    content.textContent = errorText || '';
    modal.classList.remove('hidden');
    doc.body.classList.add('modal-open');
    closeButton.focus();
}

export function hideErrorModal(doc, state) {
    const modal = doc.getElementById('error-modal');
    const content = doc.getElementById('error-modal-content');
    if (!modal || !content) {
        return;
    }
    modal.classList.add('hidden');
    content.textContent = '';
    doc.body.classList.remove('modal-open');

    const restoreFocusEl = state.modalRestoreFocusEl;
    state.modalRestoreFocusEl = null;
    if (restoreFocusEl && typeof restoreFocusEl.focus === 'function' && doc.body.contains(restoreFocusEl)) {
        restoreFocusEl.focus();
    }
}
