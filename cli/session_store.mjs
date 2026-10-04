import { randomBytes } from 'node:crypto';
import { constants } from 'node:fs';
import { open, lstat, mkdir, readFile, rename, unlink } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { CliError, EXIT } from './errors.mjs';

const SESSION_NAME = 'session.json';
const MAX_SESSION_BYTES = 4096;

function invalidState() {
    return new CliError('unsafe_session_file', '本机会话文件或目录权限不安全，请检查私有配置目录。', EXIT.auth);
}

function checkPrivate(stat, type) {
    if (type === 'directory' ? !stat.isDirectory() : !stat.isFile() || stat.nlink !== 1) throw invalidState();
    if ((stat.mode & 0o7777) !== (type === 'directory' ? 0o700 : 0o600)) throw invalidState();
    if (typeof process.getuid === 'function' && stat.uid !== process.getuid()) throw invalidState();
}

async function maybeStat(path) {
    try { return await lstat(path); }
    catch (error) { if (error.code === 'ENOENT') return null; throw error; }
}

function validSession(value) {
    return value && typeof value === 'object' &&
        typeof value.origin === 'string' && /^https?:\/\/[^/]+$/.test(value.origin) &&
        typeof value.cookie === 'string' && /^[A-Za-z0-9_-]{43}$/.test(value.cookie) &&
        typeof value.csrf === 'string' && /^[A-Za-z0-9_-]{43}$/.test(value.csrf) &&
        (value.expiresAt === undefined || typeof value.expiresAt === 'string');
}

export function createSessionStore(directory = join(homedir(), '.config', 'mediactl')) {
    const file = join(directory, SESSION_NAME);

    async function checkDirectory(create = false) {
        let stat = await maybeStat(directory);
        if (!stat && create) {
            try { await mkdir(directory, { mode: 0o700, recursive: true }); }
            catch (error) { if (error.code !== 'EEXIST') throw error; }
            stat = await maybeStat(directory);
        }
        if (stat) checkPrivate(stat, 'directory');
        return Boolean(stat);
    }

    async function load() {
        if (!await checkDirectory()) return null;
        const before = await maybeStat(file);
        if (!before) return null;
        checkPrivate(before, 'file');
        if (before.size > MAX_SESSION_BYTES) throw invalidState();
        let handle;
        try {
            handle = await open(file, constants.O_RDONLY | constants.O_NOFOLLOW);
            const after = await handle.stat();
            checkPrivate(after, 'file');
            if (before.ino !== after.ino || before.dev !== after.dev) throw invalidState();
            const contents = await readFile(handle, { encoding: 'utf8' });
            if (Buffer.byteLength(contents) > MAX_SESSION_BYTES) throw invalidState();
            const session = JSON.parse(contents);
            if (!validSession(session)) throw invalidState();
            return session;
        } catch (error) {
            if (error instanceof CliError) throw error;
            throw invalidState();
        } finally { await handle?.close(); }
    }

    async function save(session) {
        if (!validSession(session)) throw invalidState();
        await checkDirectory(true);
        const existing = await maybeStat(file);
        if (existing) checkPrivate(existing, 'file');
        const temporary = join(directory, `.session-${randomBytes(12).toString('hex')}.tmp`);
        let handle;
        try {
            handle = await open(temporary, constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW, 0o600);
            await handle.writeFile(JSON.stringify(session));
            await handle.sync();
            await handle.close();
            handle = null;
            await rename(temporary, file);
            const dirHandle = await open(directory, constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
            try { await dirHandle.sync(); } finally { await dirHandle.close(); }
        } catch (error) {
            await handle?.close().catch(() => {});
            await unlink(temporary).catch(() => {});
            if (error instanceof CliError) throw error;
            throw new CliError('session_write_failed', '无法安全保存本机会话。', EXIT.unavailable);
        }
    }

    async function clear() {
        if (!await checkDirectory()) return;
        const existing = await maybeStat(file);
        if (!existing) return;
        checkPrivate(existing, 'file');
        await unlink(file);
    }

    return { load, save, clear, directory };
}
