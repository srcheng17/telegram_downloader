const errorMessages = Object.freeze({
    disabled: '请先在设置的「连接」中配置 Komga。',
    credential_unavailable: '暂时无法读取 Komga 凭据，请检查服务端配置。',
    unauthorized: 'Komga 拒绝了凭据，请在设置的「连接」中检查授权。',
    forbidden: '当前 Komga 凭据没有读取书库的权限。',
    not_found: '该书库或作品已不存在，请刷新列表。',
    unsupported_version: 'Komga 版本暂不支持此页面所需的接口。',
    unreachable: '无法连接 Komga，请检查连接地址与网络。',
    timeout: '连接 Komga 超时，请稍后重试。',
    rate_limited: 'Komga 请求过于频繁，请稍后重试。',
    redirect_blocked: 'Komga 地址要求重定向，已停止请求。',
    response_too_large: 'Komga 返回内容超出允许范围。',
    invalid_response: 'Komga 返回内容无效，请稍后重试。',
    unavailable: 'Komga 暂时不可用，请稍后重试。',
    conflict: '文件或元数据已变化，请重新读取并核对。',
    unsafe: '文件不满足安全写回条件，已停止操作。',
    invalid_input: '提交内容无效，请检查字段和版本。',
});

export function messageForFailure(result, fallback = '作品库暂时无法加载，请稍后重试。') {
    const code = result?.payload?.code || result?.payload?.error;
    if (typeof code === 'string' && errorMessages[code]) return errorMessages[code];
    switch (result?.response?.status) {
        case 401: return '登录已失效，请重新登录。';
        case 403: return '当前账号没有读取作品库的权限。';
        case 404: return '该书库或作品已不存在，请刷新列表。';
        case 409: return '文件或元数据已变化，请重新读取并核对。';
        default: return fallback;
    }
}
