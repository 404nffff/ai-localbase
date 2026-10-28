import { describe, expect, it } from 'vitest'
import type { AppConfig } from '../../App'
import { getSettingsConfigFingerprint, hasSettingsConfigChanges, validateSettingsConfig } from './SettingsPanel'

const createConfig = (): AppConfig => ({
  wiki: { modelTimeoutSeconds: 180 },
  chat: {
    provider: 'ollama',
    baseUrl: 'http://localhost:11434/v1',
    model: 'llama3.2',
    apiKey: '',
    temperature: 0.7,
    contextMessageLimit: 12,
  },
  embedding: {
    provider: 'ollama',
    baseUrl: 'http://localhost:11434/v1',
    model: 'nomic-embed-text',
    apiKey: '',
  },
})

describe('SettingsPanel draft state', () => {
  it('validates Wiki timeout and detects unsaved changes', () => {
    const baseline = createConfig()
    const draft = createConfig()
    expect(validateSettingsConfig(draft)).toBeNull()
    draft.wiki.modelTimeoutSeconds = 300
    expect(hasSettingsConfigChanges(baseline, draft)).toBe(true)
    for (const value of [30, 1800]) {
      draft.wiki.modelTimeoutSeconds = value
      expect(validateSettingsConfig(draft)).toBeNull()
    }
    for (const value of [0, 29, 1801, 30.5, NaN, Infinity]) {
      draft.wiki.modelTimeoutSeconds = value
      expect(validateSettingsConfig(draft)).toContain('整数秒')
    }
  })
  it('treats identical values as saved', () => {
    const baseline = createConfig()
    const draft = createConfig()

    expect(getSettingsConfigFingerprint(draft)).toBe(getSettingsConfigFingerprint(baseline))
    expect(hasSettingsConfigChanges(baseline, draft)).toBe(false)
  })

  it('detects changes in either config section', () => {
    const baseline = createConfig()
    const chatDraft = createConfig()
    chatDraft.chat.model = 'qwen3'
    const embeddingDraft = createConfig()
    embeddingDraft.embedding.model = 'bge-m3'

    expect(hasSettingsConfigChanges(baseline, chatDraft)).toBe(true)
    expect(hasSettingsConfigChanges(baseline, embeddingDraft)).toBe(true)
  })

  it('returns to clean state after restoring baseline values', () => {
    const baseline = createConfig()
    const draft = createConfig()
    draft.chat.temperature = 0.2
    draft.chat.temperature = baseline.chat.temperature

    expect(hasSettingsConfigChanges(baseline, draft)).toBe(false)
  })
})
