import { constants, writeSync } from 'node:fs';
import { open } from 'node:fs/promises';
import tty from 'node:tty';
import { inflateSync } from 'node:zlib';
import { CliError, EXIT } from './errors.mjs';

const signature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
const MAX_PNG_BYTES = 32 * 1024;
const MAX_PIXELS = 512 * 512;

function invalidQR() { return new CliError('invalid_qr', '登录二维码格式无效，请刷新尝试。', EXIT.transport); }

function unpackSample(row, index, depth) {
    if (depth === 8) return row[index];
    const mask = (1 << depth) - 1;
    const shift = 8 - depth - (index * depth) % 8;
    return (row[Math.floor(index * depth / 8)] >> shift) & mask;
}

function unfilter(filter, current, previous, pixelBytes) {
    if (filter > 4) throw invalidQR();
    for (let index = 0; index < current.length; index += 1) {
        const left = index >= pixelBytes ? current[index - pixelBytes] : 0;
        const above = previous ? previous[index] : 0;
        const upperLeft = previous && index >= pixelBytes ? previous[index - pixelBytes] : 0;
        let predictor = 0;
        if (filter === 1) predictor = left;
        if (filter === 2) predictor = above;
        if (filter === 3) predictor = Math.floor((left + above) / 2);
        if (filter === 4) {
            const base = left + above - upperLeft;
            const a = Math.abs(base - left), b = Math.abs(base - above), c = Math.abs(base - upperLeft);
            predictor = a <= b && a <= c ? left : b <= c ? above : upperLeft;
        }
        current[index] = (current[index] + predictor) & 0xff;
    }
}

export function decodeQRPNG(dataURL) {
    if (typeof dataURL !== 'string' || dataURL.length > 24_000 || !/^data:image\/png;base64,[A-Za-z0-9+/=]+$/.test(dataURL)) throw invalidQR();
    const encoded = dataURL.slice('data:image/png;base64,'.length);
    const png = Buffer.from(encoded, 'base64');
    if (png.length > MAX_PNG_BYTES || !png.subarray(0, 8).equals(signature)) throw invalidQR();
    let offset = 8;
    let header = null;
    let palette = null;
    const chunks = [];
    let ended = false;
    while (offset + 12 <= png.length) {
        const length = png.readUInt32BE(offset);
        const type = png.toString('ascii', offset + 4, offset + 8);
        if (length > MAX_PNG_BYTES || offset + 12 + length > png.length) throw invalidQR();
        const data = png.subarray(offset + 8, offset + 8 + length);
        offset += 12 + length;
        if (type === 'IHDR') {
            if (header || length !== 13) throw invalidQR();
            header = {
                width: data.readUInt32BE(0), height: data.readUInt32BE(4), depth: data[8],
                color: data[9], compression: data[10], filter: data[11], interlace: data[12],
            };
        } else if (type === 'PLTE') palette = data;
        else if (type === 'IDAT') chunks.push(data);
        else if (type === 'IEND') { ended = true; break; }
        else if (type[0] === type[0].toUpperCase()) throw invalidQR();
    }
    if (!header || !ended || !chunks.length || header.width < 21 || header.height < 21 || header.width * header.height > MAX_PIXELS ||
        header.width !== header.height || header.compression !== 0 || header.filter !== 0 || header.interlace !== 0) throw invalidQR();
    const channels = { 0: 1, 2: 3, 3: 1, 4: 2, 6: 4 }[header.color];
    if (!channels || ![1, 2, 4, 8].includes(header.depth) || (header.depth !== 8 && ![0, 3].includes(header.color))) throw invalidQR();
    if (header.color === 3 && (!palette || palette.length % 3 !== 0)) throw invalidQR();
    const rowBytes = Math.ceil(header.width * channels * header.depth / 8);
    const pixelBytes = Math.max(1, Math.ceil(channels * header.depth / 8));
    const expected = (rowBytes + 1) * header.height;
    let inflated;
    try { inflated = inflateSync(Buffer.concat(chunks), { maxOutputLength: expected }); }
    catch { throw invalidQR(); }
    if (inflated.length !== expected) throw invalidQR();
    const pixels = new Uint8Array(header.width * header.height);
    let previous = null;
    let cursor = 0;
    for (let y = 0; y < header.height; y += 1) {
        const filter = inflated[cursor++];
        const row = Buffer.from(inflated.subarray(cursor, cursor + rowBytes));
        cursor += rowBytes;
        unfilter(filter, row, previous, pixelBytes);
        for (let x = 0; x < header.width; x += 1) {
            let light;
            if (header.color === 0) light = unpackSample(row, x, header.depth) * 255 / ((1 << header.depth) - 1);
            else if (header.color === 3) {
                const index = unpackSample(row, x, header.depth) * 3;
                if (index + 2 >= palette.length) throw invalidQR();
                light = (palette[index] + palette[index + 1] + palette[index + 2]) / 3;
            } else {
                const base = x * channels;
                light = header.color === 4 ? row[base] : (row[base] + row[base + 1] + row[base + 2]) / 3;
            }
            pixels[y * header.width + x] = light < 128 ? 1 : 0;
        }
        previous = row;
    }
    return { width: header.width, height: header.height, pixels };
}

export function renderQR(dataURL, columns = 80) {
    const image = decodeQRPNG(dataURL);
    if (!Number.isSafeInteger(columns) || columns < 64) throw new CliError('terminal_too_narrow', '终端宽度不足，请扩大到至少 64 列后重试。', EXIT.interaction);
    const cells = Math.min(80, columns - 6);
    const dark = (x, y) => {
        const sourceX = Math.min(image.width - 1, Math.floor((x + 0.5) * image.width / cells));
        const sourceY = Math.min(image.height - 1, Math.floor((y + 0.5) * image.height / cells));
        return image.pixels[sourceY * image.width + sourceX] === 1;
    };
    const rows = [];
    for (let y = 0; y < cells; y += 2) {
        let line = '  ';
        for (let x = 0; x < cells; x += 1) {
            const top = dark(x, y), bottom = y + 1 < cells && dark(x, y + 1);
            line += top ? bottom ? '█' : '▀' : bottom ? '▄' : ' ';
        }
        rows.push(`\x1b[47m\x1b[30m${line}  \x1b[0m`);
    }
    return `\n${rows.join('\n')}\n`;
}

export async function openQRTerminal() {
    let handle;
    try { handle = await open('/dev/tty', constants.O_RDWR | constants.O_NOCTTY); }
    catch { throw new CliError('interaction_required', '无法打开用户终端显示二维码。', EXIT.interaction); }
    const output = new tty.WriteStream(handle.fd);
    if (!output.isTTY || (output.columns && output.columns < 64)) {
        await handle.close().catch(() => {});
        throw new CliError('interaction_required', '需要至少 64 列的用户终端显示二维码。', EXIT.interaction);
    }
    let shownLines = 0;
    const write = (value) => writeSync(handle.fd, value);
    return {
        show(dataURL) {
            this.clear();
            const rendered = renderQR(dataURL, output.columns || 80);
            write('请使用 Telegram 手机客户端扫码并确认：');
            write(rendered);
            shownLines = rendered.split('\n').length - 1;
        },
        clear() {
            if (!shownLines) return;
            write(`\x1b[${shownLines}A\x1b[0J`);
            shownLines = 0;
        },
        note(message) { write(`${message}\n`); },
        async close() { this.clear(); await handle.close().catch(() => {}); },
    };
}
