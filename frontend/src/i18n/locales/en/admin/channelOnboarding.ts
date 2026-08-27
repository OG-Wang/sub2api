export default {
  channelOnboarding: {
    title: 'One-click Channel Onboarding',
    description: 'Enter upstream details to create the group, API-key account, admin API key, and channel monitor automatically.',
    form: {
      name: 'Channel name',
      namePlaceholder: 'e.g. OpenAI primary',
      platform: 'Platform',
      rateMultiplier: 'Rate multiplier',
      rateMultiplierHint: 'Any value greater than 0 is accepted; it is not restricted to 0.01 increments.',
      baseUrl: 'Upstream Base URL',
      baseUrlPlaceholder: 'https://api.example.com',
      apiKey: 'Upstream API Key',
      apiKeyPlaceholder: 'Enter the upstream API key',
      apiKeyHint: 'The key is only stored as the new account credential. It is never logged and never echoed back in the success result.',
      primaryModel: 'Primary model',
      primaryModelPlaceholder: 'e.g. gpt-4o-mini',
      intervalSeconds: 'Check interval (seconds)',
      intervalSecondsHint: 'Defaults to 900 seconds; accepted range is 15–3600 seconds.',
      expectedTokens: 'Expected input tokens (optional)',
      expectedTokensPlaceholder: 'Empty = auto-learn',
      expectedTokensHint: 'When empty, the monitor learns the baseline from its history.'
    },
    autoConfig: {
      title: 'Automatic configuration',
      standardGroup: 'Creates an enabled, standard, non-exclusive group with the selected multiplier.',
      apiKeyAccount: 'Creates an API-key account with the same name, links it to the new group, and enables upstream rate probing and sync.',
      adminKey: 'Creates a new group API key owned by the current administrator.',
      monitor: 'Creates a same-name monitor with the selected interval, public visibility, and the configured API base URL as its probe target.'
    },
    submit: 'Start onboarding',
    submitting: 'Onboarding...',
    error: 'Channel onboarding failed',
    success: {
      toast: 'Channel onboarded successfully',
      title: 'Channel onboarded successfully',
      description: 'All four resources were created in one transaction.',
      groupId: 'Group ID',
      accountId: 'Account ID',
      apiKeyId: 'API key ID',
      monitorId: 'Monitor ID',
      keyMasked: 'New group API key (masked)',
      keyHint: 'For security, the full API key is never shown on this page.'
    }
  }
}
