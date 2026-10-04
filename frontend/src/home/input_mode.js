function normalizeMode(value) {
    const mode = String(value || '').trim().toLowerCase();
    return mode === 'upload' ? 'upload' : 'url';
}

function setVisibility(node, isVisible) {
    if (!node || !node.classList) {
        return;
    }
    if (typeof node.classList.toggle === 'function') {
        node.classList.toggle('is-hidden', !isVisible);
    } else if (isVisible) {
        node.classList.remove('is-hidden');
    } else {
        node.classList.add('is-hidden');
    }
    if (typeof node.setAttribute === 'function') {
        node.setAttribute('aria-hidden', isVisible ? 'false' : 'true');
    }
}

export function applyInputMode(doc, mode) {
    const normalizedMode = normalizeMode(mode);
    const urlGroup = doc.getElementById('url-input-group');
    const archiveGroup = doc.getElementById('archive-input-group');
    const urlInput = doc.getElementById('url');
    const archiveInput = doc.getElementById('archive_file');

    const isUpload = normalizedMode === 'upload';
    setVisibility(urlGroup, normalizedMode === 'url');
    setVisibility(archiveGroup, isUpload);

    if (urlInput) {
        urlInput.disabled = normalizedMode !== 'url';
        urlInput.required = normalizedMode === 'url';
    }
    if (archiveInput) {
        archiveInput.disabled = !isUpload;
        archiveInput.required = isUpload;
    }
    return normalizedMode;
}

export function getSelectedMode(doc) {
    if (!doc || typeof doc.querySelector !== 'function') {
        return 'url';
    }
    const selected = doc.querySelector('input[name="input_mode"]:checked');
    return selected ? normalizeMode(selected.value) : 'url';
}
