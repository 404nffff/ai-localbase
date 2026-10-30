import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { ChatMessageMetadata } from '../../App'
import MessageCard from './MessageCard'

/** 通过真实消息卡片验证历史元数据的来源聚合，不改写原始证据。 */
function renderSources(sources: ChatMessageMetadata['sources']) {
  return renderToStaticMarkup(createElement(MessageCard, {
    message: { id: 'message-1', role: 'assistant', content: '测试回答', timestamp: '2026-10-30T08:00:00Z', metadata: { sources } },
    isStreamingPlaceholder: false, copiedMessageId: null, onCopyMessage: async () => {},
  }))
}

describe('知识页来源入口', () => {
  it('同页的上百条引文合并为一个按钮并使用知识页名称', () => {
    const source = { knowledgeBaseId: 'kb-1', pageId: 'wiki-1', wikiRevision: '2', documentId: 'doc-34', documentName: '' }
    const html = renderSources([
      ...Array.from({ length: 100 }, (_, index) => ({ ...source, quote: `引文 ${index}` })),
      { ...source, documentId: 'wiki-1', documentName: '员工岗位说明' },
    ])
    expect(html.match(/查看原文依据/g)).toHaveLength(1)
    expect(html).toContain('员工岗位说明')
    expect(html).not.toContain('doc-34')
  })

  it('不同库或修订保留独立入口，超过三个默认收起', () => {
    const source = { knowledgeBaseId: 'kb-1', pageId: 'wiki-1', wikiRevision: '1', documentId: 'wiki-1', documentName: '知识页一' }
    const html = renderSources([source, { ...source, knowledgeBaseId: 'kb-2' }, { ...source, wikiRevision: '2' }, { ...source, pageId: 'wiki-3', documentId: 'wiki-3', documentName: '知识页三' }])
    expect(html.match(/查看原文依据/g)).toHaveLength(3)
    expect(html).toContain('展开其余 1 个来源')
    expect(html).not.toContain('知识页三')
  })

  it('只有普通文档或缺失定位标识时不渲染空来源区域', () => {
    const html = renderSources([{ knowledgeBaseId: 'kb-1', documentId: 'doc-1', documentName: '普通资料' }, { pageId: 'wiki-1', documentName: '无库标识' }])
    expect(html).not.toContain('message-wiki-sources')
    expect(html).not.toContain('message-sources')
  })
})
