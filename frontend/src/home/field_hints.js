export function toggleFieldHint(hintNode) {
    if (!hintNode || !hintNode.classList) {
        return false;
    }
    const shouldShow = hintNode.classList.contains('is-hidden');
    if (typeof hintNode.classList.toggle === 'function') {
        hintNode.classList.toggle('is-hidden', !shouldShow);
    } else if (shouldShow) {
        hintNode.classList.remove('is-hidden');
    } else {
        hintNode.classList.add('is-hidden');
    }
    if (typeof hintNode.setAttribute === 'function') {
        hintNode.setAttribute('aria-hidden', shouldShow ? 'false' : 'true');
    }
    return shouldShow;
}

export function bindFieldHintToggles(doc) {
    if (!doc || typeof doc.querySelectorAll !== 'function') {
        return () => {};
    }
    const bindings = [];
    doc.querySelectorAll('[data-field-hint-target]').forEach((button) => {
        const targetId = button.getAttribute('data-field-hint-target');
        const hintNode = targetId ? doc.getElementById(targetId) : null;
        if (!hintNode) {
            return;
        }
        const handler = () => toggleFieldHint(hintNode);
        button.addEventListener('click', handler);
        bindings.push(() => button.removeEventListener('click', handler));
    });
    return () => bindings.forEach((unbind) => unbind());
}
