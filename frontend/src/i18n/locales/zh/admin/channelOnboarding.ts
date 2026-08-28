export default {
  channelOnboarding: {
    title: '一键接入渠道',
    description: '填写上游信息，自动创建分组、API Key 账号、管理员 API Key 和渠道监控。',
    form: {
      name: '渠道名称',
      namePlaceholder: '例如：OpenAI 主渠道',
      platform: '平台',
      rateMultiplier: '倍率',
      rateMultiplierHint: '只需大于 0，支持任意小数精度，不限制为 0.01 步长。',
      baseUrl: '上游 Base URL',
      baseUrlPlaceholder: 'https://api.example.com',
      apiKey: '上游 API Key',
      apiKeyPlaceholder: '输入上游 API Key',
      apiKeyHint: '密钥仅用于填入新账号的凭据，不会写入日志，也不会在成功结果中回显。',
      primaryModel: '主模型',
      primaryModelPlaceholder: '例如：gpt-4o-mini',
      concurrency: '并发数',
      concurrencyHint: '账号的最大并发请求数，默认 10，最小 1。',
      intervalSeconds: '监测间隔（秒）',
      intervalSecondsHint: '默认 900 秒，可填写 15～3600 秒。',
      expectedTokens: '期望输入 Token 数量（可选）',
      expectedTokensPlaceholder: '留空则自动学习',
      expectedTokensHint: '不填写时由监控根据历史结果自动学习。'
    },
    autoConfig: {
      title: '自动配置内容',
      standardGroup: '创建标准、非专属、启用分组，并使用当前倍率。',
      apiKeyAccount: '创建同名 API Key 类型账号，自动关联新分组，并开启上游倍率探测与同步。',
      adminKey: '创建归属于当前管理员的新分组 API Key。',
      monitor: '创建同名监控：使用填写的监测间隔、公开展示、探测地址取后台配置的 API 端点地址。'
    },
    submit: '开始接入',
    submitting: '接入中...',
    error: '渠道接入失败',
    success: {
      toast: '渠道接入成功',
      title: '渠道已成功接入',
      description: '四个资源已在同一个事务中创建完成。',
      groupId: '分组 ID',
      accountId: '账号 ID',
      apiKeyId: 'API Key ID',
      monitorId: '监控 ID',
      keyMasked: '新建分组 API Key（脱敏）',
      keyHint: '出于安全原因，页面不会显示完整 API Key。'
    }
  }
}
