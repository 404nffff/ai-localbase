import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import MessageCitations from './MessageCitations'

describe('引用证据', () => {
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
