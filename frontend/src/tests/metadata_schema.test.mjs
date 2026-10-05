import test from 'node:test';
import assert from 'node:assert/strict';
import { decodeMetadataSchema, parseFieldInput, formatFieldValue } from '../shared/metadata/schema.js';
import { readHistoryDocument, candidateFromHistory } from '../shared/metadata/candidates.js';

test('date editing keeps precision and rejects invalid calendar days', () => {
    const definition = { type: 'date' };
    assert.deepEqual(parseFieldInput(definition, '2024'), { year: 2024 });
    assert.deepEqual(parseFieldInput(definition, '2024-2'), { year: 2024, month: 2 });
    assert.equal(formatFieldValue(definition, parseFieldInput(definition, '2024-2-29')), '2024-02-29');
    assert.throws(() => parseFieldInput(definition, '2023-02-29'), /日期/);
    assert.throws(() => parseFieldInput(definition, '2024-13'), /月份/);
});

test('arrays preserve names with spaces and identifiers preserve their separate schemes', () => {
    assert.deepEqual(parseFieldInput({ type: 'string[]' }, 'A B\nC D'), ['A B', 'C D']);
    assert.deepEqual(parseFieldInput({ type: 'identifiers' }, 'isbn:9780000000000\nprovider:a:b'), [{ scheme: 'isbn', value: '9780000000000' }, { scheme: 'provider', value: 'a:b' }]);
    assert.throws(() => parseFieldInput({ type: 'identifiers' }, '9780000000000'), /类型/);
    assert.throws(() => parseFieldInput({ type: 'boolean' }, ''), /请选择/);
});

test('schema rejects mismatched field keys and unsupported types', () => {
    const input = { schema_version: 1, definitions_version: 'v1', definitions: { title: { key: 'summary', type: 'string', label: '标题' } } };
    assert.throws(() => decodeMetadataSchema(input), /定义/);
    input.definitions.title.key = 'title';
    input.definitions.title.type = 'script';
    assert.throws(() => decodeMetadataSchema(input), /定义/);
});

test('history does not manufacture an effective document or merge it into the submitted view', () => {
    const document = { schema_version: 1, definitions_version: 'v1', fields: { title: { state: 'value', value: '提交值' } } };
    const entry = { metadata_document: document };
    assert.equal(readHistoryDocument(entry, 'effective'), null);
    assert.equal(readHistoryDocument(entry).fields.title.value, '提交值');
    entry.effective_metadata_document = { ...document, fields: { title: { state: 'value', value: '产物值' } } };
    const effective = readHistoryDocument(entry, 'effective');
    effective.fields.title.value = '更改副本';
    assert.equal(entry.effective_metadata_document.fields.title.value, '产物值');
    assert.equal(readHistoryDocument(entry).fields.title.value, '提交值');
});

test('history candidate strips prior manual locks and retains current revision baseline', () => {
    const document = { schema_version: 1, definitions_version: 'v1', revision: 8, fields: { title: { revision: 8 } } };
    const draft = { getSnapshot: () => document, getContext: () => ({ inputRevision: 3 }) };
    const entry = { metadata_document: { ...document, fields: { title: { state: 'value', value: '历史', revision: 4, manual_locked: true, provenance: [] } } } };
    const candidate = candidateFromHistory({ entry, draft, requestId: 'h1' });
    assert.equal(candidate.base_document_revision, 8);
    assert.equal(candidate.field_revisions.title, 8);
    assert.equal(candidate.fields.title.manual_locked, undefined);
    assert.equal(candidate.origin, 'legacy');
    entry.metadata_document.definitions_version = 'older';
    assert.throws(() => candidateFromHistory({ entry, draft, requestId: 'h2' }), /兼容/);
});

test('empty values and control characters require explicit clearing', () => {
    for (const value of ['', '  ', 'title\u0000']) assert.throws(() => parseFieldInput({ type: 'string' }, value));
    for (const type of ['string[]', 'identifiers']) assert.deepEqual(parseFieldInput({ type }, ''), []);
    assert.equal(parseFieldInput({ type: 'string' }, '段落一\n段落二'), '段落一\n段落二');
    assert.throws(() => parseFieldInput({ type: 'identifiers' }, 'INVALID:123'), /类型/);
});

test('language and public URL inputs reject values disallowed by the server', () => {
    const language = { key: 'language', type: 'string' };
    for (const value of ['zh', 'zh-Hant', 'en-US', 'ja']) assert.equal(parseFieldInput(language, value), value);
    for (const value of ['Chinese', 'zh_CN', 'z', '123']) assert.throws(() => parseFieldInput(language, value), /语言/);
    const web = { key: 'web', type: 'string' };
    for (const value of ['https://example.org/books/1', 'https://example.org/?page=2', 'https://[2606:4700:4700::1111]/']) assert.equal(parseFieldInput(web, value), value);
    for (const value of ['https://user:pass@example.org/', 'https://@example.org/', 'http://127.0.0.1/', 'http://192.168.1.2/', 'http://[::1]/', 'http://[fc00::1]/', 'http://[::ffff:192.168.1.2]/', 'http://localhost/', 'http://model.internal/', 'http://private/', 'https://example.org/?api_key=secret', 'https://example.org/?key=secret', 'https://example.org/%GG', 'https://example.org/?a=1;b=2']) assert.throws(() => parseFieldInput(web, value), /公开/);
});
