#!/usr/bin/env node
import { readSync } from 'node:fs';
import { spawnSync } from 'node:child_process';

// Read exactly one JSON line, leaving any following CLI stdin untouched.
const limit = 64 * 1024;
const byte = Buffer.alloc(1);
const parts = [];
let ended = false;
while (parts.length <= limit) {
    const read = readSync(0, byte, 0, 1, null);
    if (!read) break;
    if (byte[0] === 10) { ended = true; break; }
    parts.push(byte[0]);
}

let args;
try { args = JSON.parse(Buffer.from(parts).toString('utf8')); }
catch { args = null; }
if (!ended || parts.length > limit || !Array.isArray(args) || args.length > 128 ||
    args.some((arg) => typeof arg !== 'string' || arg.includes('\0'))) {
    process.stderr.write('参数数组无效。\n');
    process.exitCode = 2;
} else {
    const result = spawnSync('mediactl', args, { stdio: 'inherit', shell: false });
    if (result.error) process.stderr.write('无法启动 mediactl；请确认已安装到 PATH。\n');
    process.exitCode = result.status ?? 6;
}
