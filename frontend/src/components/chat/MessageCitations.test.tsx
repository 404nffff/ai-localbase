import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import MessageCitations from './MessageCitations'

describe('引用证据', () => {
  it('同一分块只显示一次，不同分块和不同库的证据继续保留', () => {
    const source = { knowledgeBaseId: 'kb-1', documentId: 'doc-1', documentName: '资料', chunkId: 'chunk-1', snippet: '第一段' }
    const html = renderToStaticMarkup(createElement(MessageCitations, { metadata: { sources: [
      source, source, { ...source, chunkId: 'chunk-2', snippet: '第二段' }, { ...source, knowledgeBaseId: 'kb-2' },
    ] } }))
    expect(html.match(/<details>/g)).toHaveLength(3)
    expect(html).toContain('第二段')
  })
  it('展示可定位分块与辅助诊断，原文按文本转义', () => {
    const html = renderToStaticMarkup(createElement(MessageCitations, { metadata: {
      citationSupport: { status: 'partial', summary: '部分陈述缺少证据', coverage: 0.5 },
      sources: [{ knowledgeBaseId: 'kb-1', documentId: 'doc-1', documentName: '资料', chunkId: 'chunk-7', chunkIndex: '7', snippet: '<script>unsafe</script>' }],
    } }))
    expect(html).toContain('第 7 块')
    expect(html).toContain('部分陈述缺少证据')
    expect(html).toContain('查看原文与分块')
    expect(html).toContain('&lt;script&gt;')
    expect(html).not.toContain('<script>')
  })

  it('不把缺少真实标识的来源或 Wiki 页面冒充普通分块引用', () => {
    const html = renderToStaticMarkup(createElement(MessageCitations, { metadata: { sources: [
      { documentName: '范围概览' },
      { knowledgeBaseId: 'kb-1', documentId: 'wiki-1', documentName: 'Wiki 页面', pageId: 'wiki-1' },
    ] } }))
    expect(html).toBe('')
  })
})
