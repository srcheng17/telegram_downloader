import { createHash, randomBytes } from 'node:crypto';
import { constants } from 'node:fs';
import { open, link, unlink } from 'node:fs/promises';
import { basename, dirname, join } from 'node:path';
import { CliError, EXIT } from './errors.mjs';

const MAX_ARTIFACT_BYTES = 1024 * 1024 * 1024;
const acceptedTypes = new Set(['application/vnd.comicbook+zip', 'application/zip', 'application/octet-stream']);

function contentLength(headers) {
    if (headers['content-length'] === undefined) return null;
    const text = String(headers['content-length']);
    if (!/^(0|[1-9][0-9]*)$/.test(text)) throw new CliError('invalid_artifact', '下载响应长度无效。', EXIT.transport);
    const length = Number(text);
    if (!Number.isSafeInteger(length)) throw new CliError('artifact_too_large', '下载文件超出限制。', EXIT.unavailable);
    return length;
}

export async function saveArtifact(response, output) {
    if (typeof output !== 'string' || !output || basename(output) === '.' || basename(output) === '..') {
        throw new CliError('invalid_input', '请指定有效的本地保存路径。');
    }
    const type = String(response.headers['content-type'] || '').split(';', 1)[0].trim().toLowerCase();
    if (!acceptedTypes.has(type)) {
        response.body.destroy();
        throw new CliError('invalid_artifact', '下载响应不是受支持的归档类型。', EXIT.transport);
    }
    const expected = contentLength(response.headers);
    if (expected !== null && expected > MAX_ARTIFACT_BYTES) {
        response.body.destroy();
        throw new CliError('artifact_too_large', '下载文件超出限制。', EXIT.unavailable);
    }
    const directory = dirname(output);
    const temporary = join(directory, `.mediactl-${randomBytes(12).toString('hex')}.tmp`);
    let handle;
    let bytes = 0;
    let linked = false;
    let signature = Buffer.alloc(0);
    const hash = createHash('sha256');
    try {
        handle = await open(temporary, constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW, 0o600);
        for await (const chunk of response.body) {
            bytes += chunk.length;
            if (bytes > MAX_ARTIFACT_BYTES) throw new CliError('artifact_too_large', '下载文件超出限制。', EXIT.unavailable);
            if (signature.length < 4) signature = Buffer.concat([signature, chunk.subarray(0, 4 - signature.length)]);
            hash.update(chunk);
            await handle.writeFile(chunk);
        }
        if (expected !== null && bytes !== expected) throw new CliError('artifact_truncated', '下载文件长度不一致。', EXIT.transport);
        if (!signature.equals(Buffer.from([0x50, 0x4b, 0x03, 0x04]))) {
            throw new CliError('invalid_artifact', '下载文件不是有效的 ZIP/CBZ。', EXIT.transport);
        }
        await handle.sync();
        await handle.close();
        handle = null;
        try { await link(temporary, output); linked = true; }
        catch (error) {
            if (error.code === 'EEXIST') throw new CliError('target_exists', '目标文件已存在，未覆盖。', EXIT.conflict);
            throw error;
        }
        await unlink(temporary);
        const parent = await open(directory, constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW);
        try { await parent.sync(); } finally { await parent.close(); }
        return { saved: true, bytes, sha256: hash.digest('hex') };
    } catch (error) {
        response.body.destroy();
        await handle?.close().catch(() => {});
        await unlink(temporary).catch(() => {});
        if (error instanceof CliError) throw error;
        if (linked) throw new CliError('artifact_commit_uncertain', '文件已写入，但目录同步未确认；请检查目标文件。', EXIT.unavailable);
        throw new CliError('artifact_write_failed', '无法安全保存下载文件。', EXIT.unavailable);
    }
}
