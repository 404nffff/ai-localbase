import { describe, expect, it } from 'vitest'
import { normalizeConversation } from './api'
import type { BackendConversation } from './api'

describe('normalizeConversation', () => {
  it('filters only legacy operational assistant messages', () => {
    const conversation: BackendConversation = {
      id: 'conversation-1',
      title: '测试会话',
      knowledgeBaseId: '',
      documentId: '',
      createdAt: '2026-07-27T00:00:00Z',
      updatedAt: '2026-07-27T00:00:03Z',
      messages: [
        {
          id: 'legacy-welcome',
          role: 'assistant',
          content:
            '你好，我是 AI LocalBase 助手。你可以先选择知识库，或者进一步选中某个文档后再提问。',
          createdAt: '2026-07-27T00:00:00Z',
        },
        {
          id: 'legacy-degraded',
          role: 'assistant',
          content: '⚠️ AI 模型调用已降级\n\n模型超时',
          createdAt: '2026-07-27T00:00:01Z',
        },
        {
          id: 'real-answer',
          role: 'assistant',
          content: '文档介绍了系统的降级设计，但当前回答来自模型。',
          createdAt: '2026-07-27T00:00:02Z',
        },
        {
          id: 'user-message',
          role: 'user',
          content: '继续说明。',
          createdAt: '2026-07-27T00:00:03Z',
        },
      ],
    }

    expect(normalizeConversation(conversation).messages.map((message) => message.id)).toEqual([
      'real-answer',
      'user-message',
    ])
  })

  it('removes legacy tool-only citation sources from stored conversations', () => {
    const conversation: BackendConversation = {
      id: 'conversation-2',
      title: '历史会话',
      knowledgeBaseId: 'kb-1',
      documentId: '',
      createdAt: '2026-07-27T00:00:00Z',
      updatedAt: '2026-07-27T00:00:01Z',
      messages: [{
        id: 'answer-1',
        role: 'assistant',
        content: '模型回答',
        createdAt: '2026-07-27T00:00:01Z',
        metadata: {
          sources: [{ toolName: 'search_knowledge_base' }],
        },
      }],
    }

    expect(normalizeConversation(conversation).messages[0].metadata).toBeUndefined()
  })
})
