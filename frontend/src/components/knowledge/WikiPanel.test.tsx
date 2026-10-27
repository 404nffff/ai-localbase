import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import WikiPanel, { formatWikiTime, wikiAutomationMessage, wikiStatusLabel } from './WikiPanel'
import { setApiAccessToken, wikiRequest } from '../../services/api'

afterEach(() => { vi.unstubAllGlobals(); setApiAccessToken('') })

describe('知识页工作区', () => {
  it('提供阅读目录和上传入口，明确 MCP 自动整理前置条件', () => {
    const html = renderToStaticMarkup(<WikiPanel knowledgeBaseId="kb-test" knowledgeBaseName="项目资料" documents={[]} libraryPicker={<select aria-label="当前知识库" />} onUploadFiles={() => {}} />)
    expect(html).toContain('自动整理')
    expect(html).toContain('aria-label="知识目录"')
    expect(html).toContain('aria-label="文库导航"')
    expect(html).toContain('上传资料')
    expect(html).toContain('自动整理默认开启，未配置第三方聊天模型时跳过整理。')
    expect(html).toContain('项目资料 的知识文库')
    expect(html).not.toContain('checked=""')
  })

  it('开启但无模型时说明跳过，有模型才提示自动排队', () => {
    const overview = { enabled: true, autoCompileReady: false, counts: {}, pendingCount: 0, jobs: [] }
    expect(wikiAutomationMessage(overview)).toContain('已跳过自动整理；原始资料正常入库')
    expect(wikiAutomationMessage({ ...overview, autoCompileReady: true })).toContain('入库后排队整理')
    expect(wikiAutomationMessage({ ...overview, enabled: false })).toContain('自动整理已关闭')
  })

  it('解释未完成状态，避免将部分结果显示为完成', () => {
    expect(wikiStatusLabel('partial')).toBe('部分完成')
    expect(wikiStatusLabel('missing-source')).toBe('来源已删除')
    expect(wikiStatusLabel('needs_split')).toBe('需要拆分资料')
  })

  it('使用上海时区格式化跨年日期并处理无效时间', () => {
    expect(formatWikiTime('2026-12-31T16:05:06Z')).toBe('2027-01-01 00:05:06')
    expect(formatWikiTime('2027-01-01T00:05:06+08:00')).toBe('2027-01-01 00:05:06')
    expect(formatWikiTime('bad')).toBe('—')
  })

  it('复用现有鉴权并传递明确库范围，不提交模型配置', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ job: { id: 'job-test' } }), { status: 202 }))
    vi.stubGlobal('fetch', fetchMock)
    setApiAccessToken('test-token')
    await wikiRequest('kb test', '/jobs', 'POST', { kind: 'ingest', documentIds: ['doc-test'], requestKey: 'request' })
    const [url, request] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/knowledge-bases/kb%20test/wiki/jobs')
    expect(request.headers.get('Authorization')).toBe('Bearer test-token')
    expect(JSON.parse(request.body)).toEqual({ kind: 'ingest', documentIds: ['doc-test'], requestKey: 'request' })
  })

  it('向调用方暴露业务冲突，不把失败转成成功', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'source_changed' }), { status: 409 })))
    await expect(wikiRequest('kb-test', '/capture', 'POST', {})).rejects.toThrow('source_changed')
  })
})
