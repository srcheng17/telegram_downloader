import assert from 'node:assert/strict';
import { mkdtemp, chmod, lstat, readFile, symlink, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { createSessionStore } from '../session_store.mjs';

const session = { origin: 'https://example.test', cookie: 'a'.repeat(43), csrf: 'b'.repeat(43), expiresAt: '2026-10-05T00:00:00Z' };

test('session store writes privately and reloads, then removes session', async (t) => {
    const parent = await mkdtemp(join(tmpdir(), 'mediactl-test-'));
    t.after(() => rm(parent, { recursive: true, force: true }));
    const directory = join(parent, 'private');
    const store = createSessionStore(directory);
    await store.save(session);
    assert.equal((await lstat(directory)).mode & 0o777, 0o700);
    assert.equal((await lstat(join(directory, 'session.json'))).mode & 0o777, 0o600);
    assert.deepEqual(await store.load(), session);
    await store.clear();
    assert.equal(await store.load(), null);
});

test('session store rejects broad file permissions and symlinks', async (t) => {
    const parent = await mkdtemp(join(tmpdir(), 'mediactl-test-'));
    t.after(() => rm(parent, { recursive: true, force: true }));
    const directory = join(parent, 'private');
    const store = createSessionStore(directory);
    await store.save(session);
    await chmod(join(directory, 'session.json'), 0o644);
    await assert.rejects(store.load(), { code: 'unsafe_session_file' });
    await chmod(join(directory, 'session.json'), 0o400);
    await assert.rejects(store.load(), { code: 'unsafe_session_file' });
    await chmod(join(directory, 'session.json'), 0o600);
    await rm(join(directory, 'session.json'));
    const outside = join(parent, 'outside');
    await writeFile(outside, JSON.stringify(session), { mode: 0o600 });
    await symlink(outside, join(directory, 'session.json'));
    await assert.rejects(store.load(), { code: 'unsafe_session_file' });
    assert.deepEqual(JSON.parse(await readFile(outside, 'utf8')), session);
});

test('session store rejects broad directory permissions', async (t) => {
    const parent = await mkdtemp(join(tmpdir(), 'mediactl-test-'));
    t.after(() => rm(parent, { recursive: true, force: true }));
    const directory = join(parent, 'private');
    const store = createSessionStore(directory);
    await store.save(session);
    await chmod(directory, 0o755);
    await assert.rejects(store.load(), { code: 'unsafe_session_file' });
});
