import { constants, writeSync } from 'node:fs';
import { open } from 'node:fs/promises';
import readline from 'node:readline';
import tty from 'node:tty';
import { CliError, EXIT } from './errors.mjs';

export function requireInteractive({ json = false, interactive = Boolean(process.stdin.isTTY && process.stderr.isTTY) } = {}) {
    if (json || !interactive) {
        throw new CliError('interaction_required', '此操作需要用户在独立终端交互完成。', EXIT.interaction);
    }
}

export async function readSecret(label) {
    let handle;
    try { handle = await open('/dev/tty', constants.O_RDWR | constants.O_NOCTTY); }
    catch { throw new CliError('interaction_required', '无法打开用户终端。', EXIT.interaction); }
    const input = new tty.ReadStream(handle.fd);
    if (!input.isTTY || typeof input.setRawMode !== 'function') {
        await handle.close();
        throw new CliError('interaction_required', '需要可隐藏输入的用户终端。', EXIT.interaction);
    }
    readline.emitKeypressEvents(input);
    let secret = '';
    writeSync(handle.fd, `${label}: `);
    input.setRawMode(true);
    input.resume();
    try {
        return await new Promise((resolve, reject) => {
            input.on('keypress', (characters, key) => {
                if (key?.ctrl && key.name === 'c') {
                    writeSync(handle.fd, '\n');
                    return reject(new CliError('cancelled', '操作已取消。', EXIT.interaction));
                }
                if (key?.name === 'return' || key?.name === 'enter') {
                    writeSync(handle.fd, '\n');
                    return resolve(secret);
                }
                if (key?.name === 'backspace') {
                    secret = secret.slice(0, -1);
                } else if (characters && !key?.ctrl && !key?.meta && characters !== '\r' && characters !== '\n') {
                    if (Buffer.byteLength(secret + characters) > 4096) {
                        return reject(new CliError('invalid_input', '秘密输入超出长度限制。'));
                    }
                    secret += characters;
                }
            });
            input.once('error', () => reject(new CliError('interaction_required', '终端输入已中断。', EXIT.interaction)));
            input.once('end', () => reject(new CliError('interaction_required', '终端输入已结束。', EXIT.interaction)));
        });
    } finally {
        secret = '';
        input.pause();
        input.setRawMode(false);
        await handle.close().catch(() => {});
    }
}
