#!/usr/bin/env node
import { readFile } from 'node:fs/promises';
import { realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { runAuth } from './auth.mjs';
import { runOverview, runTasks } from './tasks.mjs';
import { runTaskErrorReview } from './task_error.mjs';
import { runSettings } from './settings.mjs';
import { runTelegram } from './telegram.mjs';
import { runKomga, runKomgaConnection } from './komga.mjs';
import { runWorkspace, runWorkspaceJSONL, runMetadata } from './workspace.mjs';
import { closeAllWorkspaces } from './workspace_broker.mjs';
import { createSessionStore } from './session_store.mjs';
import { CliError, EXIT, errorEnvelope, resultEnvelope, safeError } from './errors.mjs';

const HELP = `mediactl — 媒体工作台命令行\n\n用法:\n  mediactl [--server URL] [--allow-insecure-loopback] [--json] auth <命令>\n  mediactl [连接选项] overview\n  mediactl [连接选项] tasks <命令> [命令选项]\n  mediactl [连接选项] settings <分类> <命令> [命令选项]\n  mediactl [连接选项] connections telegram|komga <命令>\n  mediactl [连接选项] komga <分类> <命令>\n  mediactl --help | --version\n\n认证命令:\n  auth login            在独立终端隐藏输入密码并登录\n  auth status           核验当前会话\n  auth logout           撤销当前会话\n  auth password-change  在独立终端修改管理员密码\n\n任务命令:\n  tasks list            [--status STATUS] [--query TEXT] [--page N] [--per-page N]\n  tasks show|wait       --id ID（wait 可加 --timeout 秒 --interval 秒）\n  tasks error-review    --id ID（错误正文仅在独立终端显示）\n  tasks cancel|retry    --id ID\n  tasks artifact-check  --id ID\n  tasks artifact-get    --id ID --output PATH\n  tasks copy-to-komga   --id ID\n  tasks create-url      --url URL [--force]，或 --input-json - / --input-file PATH\n  tasks upload          --file PATH [--input-json - / --input-file PATH]\n\n设置命令:\n  settings download get|set                  （set 使用 --input-json - 或 --input-file PATH）\n  settings sources list|review|set|test      （review/set/test 使用 --provider ID）\n  settings ai get|review|set|models|test     （models/test 使用 --config-version N）\n  settings fields get|review|set\n  settings rules get|review|set|preview      （preview 在本机执行，不发送文字）\n  来源/AI 凭据替换仅在独立终端使用 --credential replace；也支持 keep/clear。\n  AI test 另需 --model ID。设置写入 JSON 包含服务端期望版本。\n\n连接命令:\n  connections telegram status|verify|login\n  connections telegram cancel --attempt-id ID\n\n首次登录需要 --server 指定工作台 HTTPS Origin。本机 HTTP 还需要\n--allow-insecure-loopback；每次使用该地址都需要再次显式传入。\nJSON 输入只用于普通业务数据，不接受密码或密钥。`;
const WORKSPACE_HELP = `\n\n元数据与截图：\n  metadata schema|history|providers|validate\n  workspace start\n  workspace status|review|stop --workspace ID\n  workspace attach --workspace ID --jsonl  （逐行 JSON 请求与安全摘要）\n  workspace image add --workspace ID --revision N --image PATH [--image PATH] [--clipboard]\n  workspace image languages|edit|accept-raw|retry|remove|move|cancel\n  workspace text set、field set|clear|unlock、rules preview、merge\n  workspace provider list|search|resolve、history list|select\n  workspace candidate preview|adopt、ai prepare|edit|retain|extract、validate|export\n  workspace tasks create-url --workspace ID --revision N --url URL [--force]\n  workspace tasks upload --workspace ID --revision N --file PATH\n  上述任务命令也可用 --input-json - / --input-file PATH 传入 URL 或文件路径。\n  改动时传 --revision；文字和值由 --input-json - 或 --input-file 传入。\n  tasks 从同一工作区提交已采用的草稿；未完成图片需显式 --accept-partial。\n  review 和 ai extract 仅在独立用户终端执行，默认 JSON 不返回原文。`;

const KOMGA_HELP = [
    '', '', 'Komga 作品库：',
    '  connections komga status|review|set|test',
    '  komga libraries list',
    '  komga books list --library-id ID [--query TEXT] [--page N] [--size N]',
    '  komga books show|edit|review --id ID',
    '  komga metadata preview|review|update --id ID --input-json -（或 --input-file PATH）',
    '  komga edits status|sync|restore --id OPERATION_ID',
    '  连接 set 的 JSON 包含 expected_version 和 base_url，test 使用 --config-version。',
    '  --credential replace 仅在独立终端隐藏输入 API Key；review 正文仅写入独立终端。',
].join('\n');
const REVEAL_HELP = [
    '', '', '受控内容展示：',
    '  workspace reveal prepare --workspace ID --revision N --kind merged|image|candidate|field|record [--id ID]',
    '  --json workspace reveal consume --workspace ID --revision N --ticket UUID --to-ai-context',
    '  只有用户明确要求 AI 解读内容时使用；consume 会把所选内容单次输出到 AI 上下文。',
].join('\n');

export function parseArgs(argv) {
    const options = { json: false, allowInsecureLoopback: false, server: undefined };
    const positional = [];
    const valued = new Map([
        ['--server', 'server'], ['--id', 'id'], ['--url', 'url'], ['--file', 'file'],
        ['--output', 'output'], ['--status', 'status'], ['--query', 'query'],
        ['--page', 'page'], ['--per-page', 'perPage'], ['--timeout', 'timeout'],
        ['--interval', 'interval'], ['--input-file', 'inputFile'], ['--input-json', 'inputJson'],
        ['--provider', 'provider'], ['--credential', 'credential'], ['--config-version', 'configVersion'],
        ['--model', 'model'], ['--attempt-id', 'attemptId'],
        ['--workspace', 'workspace'], ['--revision', 'revision'], ['--image-id', 'imageId'],
        ['--languages', 'languages'], ['--direction', 'direction'], ['--field', 'field'],
        ['--candidate-id', 'candidateId'], ['--keys', 'keys'], ['--result-id', 'resultId'],
        ['--entry-id', 'entryId'], ['--view', 'view'], ['--limit', 'limit'],
        ['--library-id', 'libraryId'], ['--size', 'size'],
        ['--kind', 'kind'], ['--ticket', 'ticket'],
    ]);
    for (let index = 0; index < argv.length; index += 1) {
        const arg = argv[index];
        if (arg === '--json') options.json = true;
        else if (arg === '--jsonl') options.jsonl = true;
        else if (arg === '--allow-insecure-loopback') options.allowInsecureLoopback = true;
        else if (arg === '--force') options.force = true;
        else if (arg === '--clipboard') options.clipboard = true;
        else if (arg === '--accept-partial') options.acceptPartial = true;
        else if (arg === '--confirm-locked') options.confirmLocked = true;
        else if (arg === '--to-ai-context') options.toAiContext = true;
        else if (arg === '--image') {
            if (!argv[index + 1] || argv[index + 1].startsWith('--')) throw new CliError('invalid_input', '请为 --image 指定本地路径。');
            (options.images ||= []).push(argv[++index]);
        }
        else if (valued.has(arg)) {
            const key = valued.get(arg);
            if (options[key] !== undefined || !argv[index + 1] || argv[index + 1].startsWith('--')) throw new CliError('invalid_input', '命令参数缺失或重复。');
            options[key] = argv[++index];
        } else if (arg === '--help' || arg === '-h') options.help = true;
        else if (arg === '--version' || arg === '-v') options.version = true;
        else if (arg.startsWith('-')) throw new CliError('invalid_input', '命令参数无效。');
        else positional.push(arg);
    }
    if (positional.length > 3) throw new CliError('invalid_command', '命令层级无效。');
    options.command = positional[0];
    options.action = positional[1];
    options.subaction = positional[2];
    return options;
}

async function packageVersion() {
    const pkg = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
    return pkg.version;
}

function taskFeedback(action, data) {
    if (action === 'create-url' && data.needs_confirmation) return `已有可下载文件，未创建新任务。核对后可用 --force 明确重抓。`;
    if (action === 'create-url' && data.duplicate && data.active) return `该链接已有活跃任务：${data.task.id}。`;
    if (action === 'artifact-get') return `文件已保存，共 ${data.bytes} 字节。`;
    if (action === 'copy-to-komga') return `文件复制已完成；Komga 收录状态尚未验证。`;
    if (data.task) return `任务 ${data.task.id}：${data.task.status}`;
    if (data.tasks) return `读取到 ${data.tasks.length} 个任务，共 ${data.total} 个。`;
    if (data.available) return `文件可下载。`;
    return `操作完成。`;
}

export async function main(argv = process.argv.slice(2), {
    stdout = process.stdout, stderr = process.stderr,
    store = createSessionStore(), runAuthCommand = runAuth,
    runOverviewCommand = runOverview, runTasksCommand = runTasks, runTaskErrorReviewCommand = runTaskErrorReview,
    runSettingsCommand = runSettings, runTelegramCommand = runTelegram,
    runKomgaCommand = runKomga, runKomgaConnectionCommand = runKomgaConnection,
    runWorkspaceCommand = runWorkspace, runWorkspaceJSONLCommand = runWorkspaceJSONL, runMetadataCommand = runMetadata,
    closeAllWorkspacesCommand = closeAllWorkspaces,
} = {}) {
    let options;
    try {
        options = parseArgs(argv);
        if (options.jsonl && (options.json || options.command !== 'workspace' || options.action !== 'attach' || options.subaction || !options.workspace)) {
            throw new CliError('invalid_input', '--jsonl 仅支持 workspace attach --workspace ID，且不能同时使用 --json。');
        }
        if (options.toAiContext && (options.command !== 'workspace' || options.action !== 'reveal' || options.subaction !== 'consume')) {
            throw new CliError('invalid_input', '--to-ai-context 仅支持 workspace reveal consume。');
        }
        if (options.command === 'workspace' && options.action === 'reveal' && options.subaction === 'consume' && (!options.json || !options.toAiContext)) {
            throw new CliError('interaction_required', '单次展示需要同时明确 --json 和 --to-ai-context。', EXIT.interaction);
        }
        let data;
        if (options.jsonl) {
            await runWorkspaceJSONLCommand({ ...options, store }, { stdout });
            return 0;
        } else if (options.version || options.command === 'version') {
            const version = await packageVersion();
            if (options.json) data = { version };
            else stdout.write(`${version}\n`);
        } else if (options.help || !options.command || options.command === 'help') {
            if (options.json) data = { help: HELP + WORKSPACE_HELP + KOMGA_HELP + REVEAL_HELP };
            else stdout.write(`${HELP}${WORKSPACE_HELP}${KOMGA_HELP}${REVEAL_HELP}\n`);
        } else if (options.command === 'auth' && !options.subaction && ['login', 'status', 'logout', 'password-change'].includes(options.action)) {
            data = await runAuthCommand(options.action, { ...options, store });
            if (options.action === 'logout' || options.action === 'password-change') await closeAllWorkspacesCommand().catch(() => {});
            if (!options.json) stdout.write(`${data.authenticated ? '已登录' : data.password_changed ? '密码已修改，所有会话已退出' : '未登录'}\n`);
        } else if (options.command === 'overview' && !options.action) {
            data = await runOverviewCommand({ ...options, store });
            if (!options.json) stdout.write(`任务总数：${data.total_tasks}，进行中：${data.active_tasks}\n`);
        } else if (options.command === 'tasks' && options.action === 'error-review' && !options.subaction) {
            data = await runTaskErrorReviewCommand({ ...options, store });
            if (!options.json) stdout.write(data.details_reviewed ? '错误详情已在独立终端显示。\n' : '此任务无错误详情。\n');
        } else if (options.command === 'tasks' && options.action && !options.subaction) {
            data = await runTasksCommand(options.action, { ...options, store });
            if (!options.json) stdout.write(`${taskFeedback(options.action, data)}\n`);
        } else if (options.command === 'settings' && options.action && options.subaction) {
            data = await runSettingsCommand(options.action, options.subaction, { ...options, store });
            if (!options.json) stdout.write(`设置操作已完成：${options.action} ${options.subaction}。\n`);
        } else if (options.command === 'connections' && options.action === 'telegram' && options.subaction) {
            data = await runTelegramCommand(options.subaction, { ...options, store });
            if (!options.json) stdout.write(`Telegram 账号状态：${data.account?.state || data.state || data.attempt?.state}。\n`);
        } else if (options.command === 'connections' && options.action === 'komga' && options.subaction) {
            data = await runKomgaConnectionCommand(options.subaction, { ...options, store });
            if (!options.json) stdout.write(`Komga 连接：${data.status || (data.target_configured ? '已配置' : '未配置')}。\n`);
        } else if (options.command === 'komga' && options.action && options.subaction) {
            data = await runKomgaCommand(options.action, options.subaction, { ...options, store });
            if (!options.json) stdout.write(data.operation ? `Komga 操作 ${data.operation.id}：${data.operation.state}${data.verification_pending ? '（状态待回读）' : ''}。\n` : `Komga ${options.action} ${options.subaction} 已完成。\n`);
        } else if (options.command === 'metadata' && options.action && !options.subaction) {
            data = await runMetadataCommand(options.action, { ...options, store });
            if (!options.json) stdout.write(`元数据操作已完成：${options.action}。\n`);
        } else if (options.command === 'workspace' && options.action) {
            data = await runWorkspaceCommand(options.action, options.subaction, { ...options, store });
            if (!options.json && options.action === 'reveal' && options.subaction === 'prepare') {
                stdout.write(`reveal 票据：${data.ticket}；修订 ${data.revision}；内容 ${data.bytes} 字节；${data.expires_in_seconds} 秒内有效。\n`);
            } else if (!options.json) stdout.write(options.action === 'tasks' ? `${taskFeedback(options.subaction, data)}${data.snapshot_attached ? ' 已附上当前元数据快照。' : ' 草稿仍保留。'}\n` : `工作区操作已完成：${options.action}${options.subaction ? ` ${options.subaction}` : ''}。${data?.workspace_id ? ` ID ${data.workspace_id}。` : ''}${data?.revision === undefined ? '' : ` 修订 ${data.revision}。`}\n`);
        } else {
            throw new CliError('invalid_command', '未知命令，请运行 mediactl --help。');
        }
        if (options.json) stdout.write(`${JSON.stringify(resultEnvelope(data))}\n`);
        return 0;
    } catch (error) {
        const safe = safeError(error);
        if (options?.json || argv.includes('--json')) stderr.write(`${JSON.stringify(errorEnvelope(safe))}\n`);
        else stderr.write(`错误 [${safe.code}]：${safe.message}\n`);
        return safe.exitCode;
    }
}

let invokedAsMain = false;
try { invokedAsMain = Boolean(process.argv[1]) && fileURLToPath(import.meta.url) === realpathSync(resolve(process.argv[1])); }
catch { /* An imported module or removed entrypoint is not the CLI main process. */ }
if (invokedAsMain) {
    process.exitCode = await main();
}
