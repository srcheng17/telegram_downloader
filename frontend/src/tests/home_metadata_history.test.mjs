import test from 'node:test';
import assert from 'node:assert/strict';

import { applyHistoryEntryToForm } from '../home/metadata_history.js';

test('applyHistoryEntryToForm restores mode, metadata, and url for url entries', () => {
    const form = {
        controls: {
            url: { value: '' },
            author: { value: '' },
            series_name: { value: '' },
            comic_name: { value: '' },
            summary: { value: '' },
            tags: { value: '' },
            genres: { value: '' },
        },
        querySelector(selector) {
            const match = selector.match(/\[name="(.+)"\]/);
            return match ? this.controls[match[1]] || null : null;
        },
    };

    const selectedMode = applyHistoryEntryToForm(form, {
        task_type: 'url',
        url: 'https://telegra.ph/demo',
        author: '作者A',
        series_name: '系列B',
        comic_name: '漫画C',
        summary: '简介D',
        tags: 'tag1,tag2',
        genres: 'genre1',
    });

    assert.equal(selectedMode, 'url');
    assert.equal(form.controls.url.value, 'https://telegra.ph/demo');
    assert.equal(form.controls.author.value, '作者A');
    assert.equal(form.controls.series_name.value, '系列B');
    assert.equal(form.controls.comic_name.value, '漫画C');
    assert.equal(form.controls.summary.value, '简介D');
    assert.equal(form.controls.tags.value, 'tag1,tag2');
    assert.equal(form.controls.genres.value, 'genre1');
});

test('applyHistoryEntryToForm keeps upload mode entries from restoring file input', () => {
    const form = {
        controls: {
            archive_file: { value: 'keep-empty' },
            author: { value: '' },
        },
        querySelector(selector) {
            const match = selector.match(/\[name="(.+)"\]/);
            return match ? this.controls[match[1]] || null : null;
        },
    };

    const selectedMode = applyHistoryEntryToForm(form, {
        task_type: 'upload',
        author: '作者A',
    });

    assert.equal(selectedMode, 'upload');
    assert.equal(form.controls.author.value, '作者A');
    assert.equal(form.controls.archive_file.value, 'keep-empty');
});
