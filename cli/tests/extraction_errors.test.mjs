import test from 'node:test';
import assert from 'node:assert/strict';
import { httpError } from '../errors.mjs';

test('finite extraction errors retain the failed layer without exposing upstream text', () => {
    for (const [status, code] of [[502, 'invalid_response'], [502, 'refused'], [400, 'schema_unsupported'], [400, 'context_exceeded'], [400, 'input_too_large'], [400, 'invalid_request'], [503, 'disabled'], [503, 'not_configured'], [409, 'config_changed'], [499, 'cancelled'], [409, 'idempotency_conflict']]) {
        const result = httpError(status, { code, message: 'private-model-prompt' });
        assert.equal(result.code, code);
        assert.ok(!result.message.includes('private-model-prompt'));
    }
    assert.equal(httpError(502, { code: 'private-model-prompt' }).code, 'unavailable');
});
