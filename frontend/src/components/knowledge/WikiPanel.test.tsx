import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import WikiPanel, { formatWikiTime, wikiAutomationMessage, wikiFailureMessage, wikiStatusLabel, WikiJobList } from './WikiPanel'
import { fetchWikiChanges, fetchWikiPageForEdit, setApiAccessToken, wikiRequest } from '../../services/api'

afterEach(() => { vi.unstubAllGlobals(); setApiAccessToken('') })

describe('知识页工作区', () => {
  it('编辑读取完整 Unicode 正文并固定后续分页修订', async () => {
    const page = { pageId: 'page-1', title: '知识页', revision: 3, totalChars: 4 }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...page, markdown: '中😀', nextOffset: 2 })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...page, markdown: '文。', nextOffset: 0 })))
    vi.stubGlobal('fetch', fetchMock)
    const signal = new AbortController().signal
    const result = await fetchWikiPageForEdit('kb-1', 'page-1', signal)
    expect(result.markdown).toBe('中😀文。')
    expect(result.nextOffset).toBe(0)
    expect(fetchMock.mock.calls[1][0]).toContain('revision=3&offset=2')
    expect(fetchMock.mock.calls[1][1].signal).toBe(signal)
  })

  it('拒绝截断、混合修订和重复游标，不能把残缺正文带入编辑器', async () => {
    const first = { pageId: 'page-1', revision: 2, totalChars: 4, markdown: '中文', nextOffset: 2 }
    for (const part of [
      { ...first, revision: 3, markdown: '正文', nextOffset: 0 },
      { ...first, markdown: '正文', nextOffset: 2 },
      { ...first, markdown: '文', nextOffset: 0 },
    ]) {
      vi.stubGlobal('fetch', vi.fn()
        .mockResolvedValueOnce(new Response(JSON.stringify(first)))
        .mockResolvedValueOnce(new Response(JSON.stringify(part))))
      await expect(fetchWikiPageForEdit('kb-1', 'page-1', new AbortController().signal)).rejects.toThrow(/正文/)
    }
  })

  it('分页请求失败保持失败语义', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ pageId: 'page-1', revision: 2, totalChars: 4, markdown: '中文', nextOffset: 2 })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: 'revision missing' }), { status: 404 })))
    await expect(fetchWikiPageForEdit('kb-1', 'page-1', new AbortController().signal)).rejects.toThrow('revision missing')
  })
  it('全库历史使用统一分页接口并保留筛选、取消与鉴权', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [], total: 0, nextCursor: '' })))
    vi.stubGlobal('fetch', fetchMock)
    setApiAccessToken('fixture-token')
    const signal = new AbortController().signal
    await fetchWikiChanges('kb one', 20, signal)
    const [url, request] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/wiki/changes?knowledgeBaseId=kb+one&offset=20&limit=20')
    expect(request.signal).toBe(signal)
    expect(request.headers.get('Authorization')).toBe('Bearer fixture-token')
  })
  it('提供阅读目录和上传入口，明确 MCP 自动整理前置条件', () => {
    const html = renderToStaticMarkup(<WikiPanel knowledgeBaseId="kb-test" knowledgeBaseName="项目资料" documents={[]} libraryPicker={<select aria-label="当前知识库" />} onUploadFiles={() => {}} />)
    expect(html).toContain('自动整理')
    expect(html).toContain('aria-label="知识目录"')
    expect(html).toContain('aria-label="文库导航"')
    expect(html).toContain('上传资料')
    expect(html).toContain('自动整理默认开启，未配置第三方聊天模型时跳过整理。')
    expect(html).toContain('项目资料 的知识文库')
    expect(html).toContain('wiki-panel-wide')
    expect(html).not.toContain('整理后的文档记录')
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
    expect(wikiStatusLabel('needs_split')).toBe('超限待恢复')
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

describe('整理任务恢复入口', () => {
  const job = { id: 'job-1', kind: 'ingest', status: 'failed', stage: 'extract', attempt: 1, error: '模型连接超时', totalSegments: 4, processedSegments: 2, totalPages: 0, checkedPages: 0, updatedAt: '2026-12-31T16:05:06Z' }

  it('旧拆分任务可在剩余预算内恢复，并显示资料名称和具体处理说明', () => {
    const html = renderToStaticMarkup(<WikiJobList jobs={[{ ...job, status: 'needs_split', sourceNames: ['项目规则.md'], error: 'combined context exceeds compilation budget' }]} enabled busy={false} onRetry={() => {}} />)
    expect(html).toContain('项目规则.md')
    expect(html).toContain('重试失败阶段')
    expect(html).toContain('分批综合')
    expect(wikiFailureMessage('model attempted to change a page without reading its baseline')).toContain('完整基线')
    const exhausted = renderToStaticMarkup(<WikiJobList jobs={[{ ...job, modelCalls: 32 }]} enabled busy={false} onRetry={() => {}} />)
    expect(exhausted).toContain('重试失败阶段')
    expect(html).toContain('无需手动拆分原始资料')
  })

  it('展示失败原因、真实分段覆盖和可重试入口', () => {
    const html = renderToStaticMarkup(<WikiJobList jobs={[job]} enabled busy={false} onRetry={() => {}} />)
    expect(html).toContain('模型连接超时')
    expect(html).toContain('重试失败阶段')
    expect(html).toContain('value="2" max="4"')
    expect(html).toContain('2027-01-01 00:05:06')
  })

  it('累计三次仍可重试，关闭本库时不提供重试', () => {
    const exhausted = renderToStaticMarkup(<WikiJobList jobs={[{ ...job, attempt: 3 }]} enabled busy={false} onRetry={() => {}} />)
    expect(exhausted).toContain('重试失败阶段')
    expect(exhausted).toContain('累计执行 3 次')
    const disabled = renderToStaticMarkup(<WikiJobList jobs={[job]} enabled={false} busy={false} onRetry={() => {}} />)
    expect(disabled).not.toContain('重试失败阶段')
    expect(disabled).toContain('开启后可重试')
  })

  it('部分完成不能通过失败重试入口提交', () => {
    for (const status of ['partial']) {
      const html = renderToStaticMarkup(<WikiJobList jobs={[{ ...job, status, error: '' }]} enabled busy={false} onRetry={() => {}} />)
      expect(html).not.toContain('重试失败阶段')
      expect(html).toContain(wikiStatusLabel(status))
    }
  })
})
