import { describe, expect, it } from 'vitest'
import type { AppConfig } from '../../App'
import { getSettingsConfigFingerprint, hasSettingsConfigChanges } from './SettingsPanel'

const createConfig = (): AppConfig => ({
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
