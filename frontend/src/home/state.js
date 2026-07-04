export function readOptionalField(form, fieldName) {
    if (!form || !fieldName) {
        return '';
    }
    const input = form.querySelector(`[name="${fieldName}"]`);
    return input ? String(input.value || '').trim() : '';
}

export function readForceValue(form) {
    if (!form) {
        return null;
    }
    const forceControl = form.querySelector('[name="force"]');
    if (!forceControl) {
        return null;
    }
    if (forceControl.matches('input[type="checkbox"]')) {
        return forceControl.checked ? 'true' : 'false';
    }
    if (forceControl.matches('input[type="radio"]')) {
        const checkedRadio = form.querySelector('input[name="force"]:checked');
        return checkedRadio ? String(checkedRadio.value || '').trim() : '';
    }
    return String(forceControl.value || '').trim();
}

export function collectMetadataPayload(form) {
    return {
        author: readOptionalField(form, 'author'),
        series_name: readOptionalField(form, 'series_name'),
        series_number: readOptionalField(form, 'series_number'),
        comic_name: readOptionalField(form, 'comic_name'),
        summary: readOptionalField(form, 'summary'),
        tags: readOptionalField(form, 'tags'),
        genres: readOptionalField(form, 'genres'),
    };
}

export function collectFormPayload(form) {
    const payload = {
        url: readOptionalField(form, 'url'),
        ...collectMetadataPayload(form),
    };
    const forceValue = readForceValue(form);
    if (forceValue !== null) {
        payload.force = forceValue;
    }
    return payload;
}
