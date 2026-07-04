import test from 'node:test';
import assert from 'node:assert/strict';

import { collectFormPayload, collectMetadataPayload } from '../home/state.js';

test('collectMetadataPayload reads trimmed metadata fields', () => {
  const form = {
    querySelector(selector) {
      const values = {
        '[name="author"]': { value: ' 作者A ' },
        '[name="series_name"]': { value: ' 系列B ' },
        '[name="series_number"]': { value: ' 3 ' },
        '[name="comic_name"]': { value: ' 漫画C ' },
        '[name="summary"]': { value: ' 简介D ' },
        '[name="tags"]': { value: ' tag1 ' },
        '[name="genres"]': { value: ' genre1 ' },
      };
      return values[selector] || null;
    },
  };
  assert.deepEqual(collectMetadataPayload(form), {
    author: '作者A',
    series_name: '系列B',
    series_number: '3',
    comic_name: '漫画C',
    summary: '简介D',
    tags: 'tag1',
    genres: 'genre1',
  });
});

test('collectFormPayload includes force field when present', () => {
  const form = {
    querySelector(selector) {
      const values = {
        '[name="url"]': { value: ' https://telegra.ph/demo ' },
        '[name="author"]': { value: ' 作者A ' },
        '[name="series_name"]': { value: '' },
        '[name="series_number"]': { value: '' },
        '[name="comic_name"]': { value: '' },
        '[name="summary"]': { value: '' },
        '[name="tags"]': { value: '' },
        '[name="genres"]': { value: '' },
        '[name="force"]': { value: 'true', matches: () => false },
      };
      return values[selector] || null;
    },
  };
  assert.deepEqual(collectFormPayload(form), {
    url: 'https://telegra.ph/demo',
    author: '作者A',
    series_name: '',
    series_number: '',
    comic_name: '',
    summary: '',
    tags: '',
    genres: '',
    force: 'true',
  });
});
