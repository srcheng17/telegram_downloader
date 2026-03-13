import test from 'node:test';
import assert from 'node:assert/strict';

import { normalizeUploadSnapshot } from '../shared/archive_upload.js';

test('normalizeUploadSnapshot returns a safe default snapshot', () => {
    assert.deepEqual(normalizeUploadSnapshot(null), {
        status: 'idle',
        loadedBytes: 0,
        totalBytes: 0,
        fileName: '',
        errorMessage: '',
        canCancel: false,
    });
});

test('normalizeUploadSnapshot coerces values into the expected shape', () => {
    assert.deepEqual(
        normalizeUploadSnapshot({
            status: 'uploading',
            loadedBytes: '12',
            totalBytes: 25.4,
            fileName: ' demo.cbz ',
            errorMessage: ' failed ',
            canCancel: 1,
        }),
        {
            status: 'uploading',
            loadedBytes: 12,
            totalBytes: 25,
            fileName: 'demo.cbz',
            errorMessage: 'failed',
            canCancel: true,
        },
    );
});
