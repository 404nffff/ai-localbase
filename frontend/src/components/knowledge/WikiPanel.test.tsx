import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import WikiPanel, { formatWikiTime, wikiAutomationMessage, wikiStatusLabel, WikiJobList } from './WikiPanel'
import { fetchWikiChanges, setApiAccessToken, wikiRequest } from '../../services/api'

afterEach(() => { vi.unstubAllGlobals(); setApiAccessToken('') })

describe('知识页工作区', () => {
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
    expect(html).toContain('整理后的文档记录')
    expect(html).toContain('点击查看整理内容')
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

describe('整理任务恢复入口', () => {
  const job = { id: 'job-1', kind: 'ingest', status: 'failed', stage: 'extract', attempt: 1, error: '模型连接超时', totalSegments: 4, processedSegments: 2, totalPages: 0, checkedPages: 0, updatedAt: '2026-12-31T16:05:06Z' }

  it('展示失败原因、真实分段覆盖和可重试入口', () => {
    const html = renderToStaticMarkup(<WikiJobList jobs={[job]} enabled busy={false} onRetry={() => {}} />)
    expect(html).toContain('模型连接超时')
    expect(html).toContain('重试失败阶段')
    expect(html).toContain('value="2" max="4"')
    expect(html).toContain('2027-01-01 00:05:06')
  })

  it('达到上限或关闭本库时不提供重试', () => {
    const exhausted = renderToStaticMarkup(<WikiJobList jobs={[{ ...job, attempt: 3 }]} enabled busy={false} onRetry={() => {}} />)
    expect(exhausted).not.toContain('重试失败阶段')
    expect(exhausted).toContain('已达到 3 次执行上限')
    const disabled = renderToStaticMarkup(<WikiJobList jobs={[job]} enabled={false} busy={false} onRetry={() => {}} />)
    expect(disabled).not.toContain('重试失败阶段')
    expect(disabled).toContain('开启后可重试')
  })

  it('部分完成与资料拆分不能通过失败重试入口提交', () => {
    for (const status of ['partial', 'needs_split']) {
      const html = renderToStaticMarkup(<WikiJobList jobs={[{ ...job, status, error: '' }]} enabled busy={false} onRetry={() => {}} />)
      expect(html).not.toContain('重试失败阶段')
      expect(html).toContain(wikiStatusLabel(status))
    }
  })
})
