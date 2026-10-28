import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import type { DocumentItem, KnowledgeBaseFileUploadState } from '../../App'
import { downloadWiki, wikiRequest, type WikiIssue, type WikiJob, type WikiList, type WikiOverview, type WikiPage, type WikiPageContent, type WikiSourceContent, type WikiSourceRef } from '../../services/api'
import MarkdownRenderer from '../chat/MarkdownRenderer'
import AppIcon from '../common/AppIcon'
import '../../styles/wiki.css'

/** 整理和页面状态的中文说明，保留未知状态便于诊断新版本。 */
export function wikiStatusLabel(value: string): string {
  const labels: Record<string, string> = { queued: '排队中', running: '整理中', succeeded: '已完成', failed: '失败', partial: '部分完成', needs_split: '需要拆分资料', canceled: '已取消', fresh: '有效', stale: '来源已更新', 'missing-source': '来源已删除', topic: '主题', decision: '决策', 'source-summary': '资料摘要', question: '待核实', open: '待处理', resolved: '已解决', dismissed: '已忽略', completed: '完成', unavailable: '未完成', not_requested: '未请求' }
  const steps: Record<string, string> = { read_sources: '读取资料', extract: '提取事实', plan: '整理内容', validate: '校验证据', publish: '保存修订', index: '建立索引', finalize: '完成', structure: '结构检查', semantic: '语义检查', 'broken-link': '链接失效', orphan: '缺少入链', 'stale-source': '来源版本过期', 'duplicate-title': '标题重复', 'suspected-contradiction': '疑似矛盾', 'unsupported-claim': '证据不足' }
  return labels[value] ?? steps[value] ?? value
}

/** 知识维护时间统一显示为上海时区的完整日期时间。 */
export function formatWikiTime(value: string): string {
  const date = new Date(value)
  if (!value || Number.isNaN(date.getTime())) return '—'
  const parts = new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).formatToParts(date)
  const part = (name: string) => parts.find((item) => item.type === name)?.value ?? ''
  return `${part('year')}-${part('month')}-${part('day')} ${part('hour')}:${part('minute')}:${part('second')}`
}

/** 浏览单页与来源版本；历史正文逐页加载，不把预览当全文。 */
export function WikiPageViewer({ knowledgeBaseId, pageId, initialRevision = 0, onClose }: { knowledgeBaseId: string; pageId: string; initialRevision?: number; onClose: () => void }) {
  const [page, setPage] = useState<WikiPageContent | null>(null)
  const [selectedPage, setSelectedPage] = useState(pageId)
  const [revision, setRevision] = useState(initialRevision)
  const [source, setSource] = useState<WikiSourceContent | null>(null)
  const [sourceRef, setSourceRef] = useState<WikiSourceRef | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const generation = useRef(0)

  useEffect(() => {
    const controller = new AbortController()
    generation.current += 1
    setPage(null); setSource(null); setSourceRef(null); setError(''); setBusy(false)
    void wikiRequest<WikiPageContent>(knowledgeBaseId, `/pages/${encodeURIComponent(selectedPage)}?revision=${revision}`, 'GET', undefined, controller.signal)
      .then(setPage).catch((reason: Error) => { if (!controller.signal.aborted) setError(reason.message) })
    return () => controller.abort()
  }, [knowledgeBaseId, selectedPage, revision])

  /** 读取指定来源版本；切换来源会替换正文，翻页仅追加同一版本。 */
  async function readSource(ref: WikiSourceRef, offset = 0) {
    const current = ++generation.current
    setBusy(true); setError('')
    try {
      const next = await wikiRequest<WikiSourceContent>(knowledgeBaseId, `/sources/${encodeURIComponent(ref.documentId)}/revisions/${encodeURIComponent(ref.checksum)}?offset=${offset}`)
      if (current !== generation.current) return
      setSourceRef(ref)
      setSource((old) => ({ ...next, content: offset ? (old?.content ?? '') + next.content : next.content }))
    } catch (reason) { if (current === generation.current) setError(reason instanceof Error ? reason.message : '读取来源失败') }
    finally { if (current === generation.current) setBusy(false) }
  }

  /** 追加当前页面的下一段正文，保留请求时的页面身份。 */
  async function morePage() {
    if (!page) return
    const current = generation.current
    setBusy(true); setError('')
    try {
      const next = await wikiRequest<WikiPageContent>(knowledgeBaseId, `/pages/${encodeURIComponent(page.pageId)}?revision=${page.revision}&offset=${page.nextOffset}`)
      if (current === generation.current) setPage((old) => ({ ...next, markdown: (old?.markdown ?? '') + next.markdown }))
    } catch (reason) { if (current === generation.current) setError(reason instanceof Error ? reason.message : '读取页面失败') }
    finally { if (current === generation.current) setBusy(false) }
  }

  return <section className="wiki-reader" aria-label="知识页详情">
    <div className="wiki-toolbar"><h3>{page?.title ?? '读取知识页…'}</h3><button type="button" onClick={onClose}>关闭详情</button></div>
    {error && <p role="alert" className="wiki-error">{error}</p>}
    {page && <>
      <div className="wiki-toolbar"><span>{wikiStatusLabel(page.metadata.freshness)}</span><label>历史修订 <select value={revision} onChange={(event) => setRevision(Number(event.target.value))}>
        <option value={0}>当前版本</option>{Array.from({ length: page.metadata.revision }, (_, index) => <option key={index + 1} value={index + 1}>修订 {index + 1}</option>)}
      </select></label><span>共 {page.totalChars} 字符</span></div>
      <MarkdownRenderer content={page.markdown} />
      {page.nextOffset > 0 && <button disabled={busy} onClick={() => void morePage()}>加载后续正文</button>}
      <h4>原始来源</h4>
      {!page.sourceRefs?.length && <p>本页没有已验证的原始来源，请按待核实内容使用。</p>}
      {(page.sourceRefs ?? []).map((ref, index) => <div className="wiki-evidence" key={`${ref.documentId}-${index}`}><blockquote>{ref.quote}</blockquote><button disabled={busy || page.missingSources.includes(ref.documentId)} onClick={() => void readSource(ref)}>{page.missingSources.includes(ref.documentId) ? '来源已删除' : `查看来源 ${ref.documentId}`}</button><small>版本 {ref.checksum.slice(0, 12)}</small></div>)}
      {source && <section className="wiki-source"><h4>{source.name} · 原始版本</h4><pre>{source.content}</pre>{source.nextOffset > 0 && sourceRef && <button disabled={busy} onClick={() => void readSource(sourceRef, source.nextOffset)}>加载后续来源</button>}</section>}
      {(page.links ?? []).length > 0 && <div className="wiki-toolbar"><span>关联知识页</span>{page.links.map((id) => <button key={id} onClick={() => { setSelectedPage(id); setRevision(0) }}>{id}</button>)}</div>}
    </>}
  </section>
}

/** 单库阅读工作区的宿主入口，上传沿用知识库原有处理与进度。 */
interface WikiPanelProps {
  /** 当前知识库的服务端标识，用于所有 Wiki 请求。 */
  knowledgeBaseId: string
  /** 当前库名称，用于面包屑与文库首页。 */
  knowledgeBaseName: string
  /** 宿主维护的文档列表，选择资料时排除派生知识页。 */
  documents: DocumentItem[]
  /** 知识库切换控件由宿主管理，切库后重建本工作区。 */
  libraryPicker: ReactNode
  /** 交给宿主上传文件，保留既有上传错误和进度反馈。 */
  onUploadFiles: (files: FileList | null) => void
  /** 未上传时为空；上传期间禁用重复选择并显示完成数量。 */
  uploadState?: KnowledgeBaseFileUploadState
}

/**
 * 以知识目录与正文为主，资料、任务和体检在各自视图中维护。
 * 宿主负责当前库选择、原始文件上传及上传失败反馈；本组件回读 Wiki 状态，
 * 展示整理错误并轮询已运行任务。历史资料仍需显式选择，切换库由宿主 key 重建状态。
 */
export default function WikiPanel({ knowledgeBaseId, knowledgeBaseName, documents, libraryPicker, onUploadFiles, uploadState }: WikiPanelProps) {
  const [overview, setOverview] = useState<WikiOverview | null>(null)
  const [pages, setPages] = useState<WikiList<WikiPage>>({ items: [], total: 0, nextCursor: '' })
  const [issues, setIssues] = useState<WikiList<WikiIssue>>({ items: [], total: 0, nextCursor: '' })
  const [selected, setSelected] = useState<string[]>([])
  const [pageId, setPageId] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [pageType, setPageType] = useState('')
  const [freshness, setFreshness] = useState('')
  const [issueStatus, setIssueStatus] = useState('open')
  const [pageCursor, setPageCursor] = useState('')
  const [issueCursor, setIssueCursor] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [view, setView] = useState<'read' | 'sources' | 'jobs' | 'issues'>('read')
  const [sourceQuery, setSourceQuery] = useState('')
  const [directoryOpen, setDirectoryOpen] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const generation = useRef(0)

  /** 单次刷新保持列表筛选，旧请求不得覆盖后续筛选结果。 */
  const refresh = useCallback(async () => {
    const current = ++generation.current
    const pageParams = new URLSearchParams({ query, pageType, freshness, cursor: pageCursor })
    const issueParams = new URLSearchParams({ status: issueStatus, cursor: issueCursor })
    const results = await Promise.all([
      wikiRequest<WikiOverview>(knowledgeBaseId),
      wikiRequest<WikiList<WikiPage>>(knowledgeBaseId, `/pages?${pageParams}`),
      wikiRequest<WikiList<WikiIssue>>(knowledgeBaseId, `/issues?${issueParams}`),
    ])
    if (current !== generation.current) return
    setOverview(results[0]); setPages(results[1]); setIssues(results[2])
  }, [knowledgeBaseId, query, pageType, freshness, pageCursor, issueStatus, issueCursor])

  // 宿主上传后更新资料列表，同步回读新创建的整理任务和知识目录。
  useEffect(() => {
    let active = true
    void refresh().catch((reason: Error) => { if (active) setError(reason.message) })
    return () => { active = false; generation.current += 1 }
  }, [refresh, documents])

  const running = overview?.jobs.some((job) => job.status === 'queued' || job.status === 'running') ?? false
  useEffect(() => {
    if (!running) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try { await refresh() } catch (reason) { if (!stopped) setError(reason instanceof Error ? reason.message : '刷新失败') }
      if (!stopped) timer = setTimeout(() => void poll(), 2000)
    }
    timer = setTimeout(() => void poll(), 2000)
    return () => { stopped = true; clearTimeout(timer) }
  }, [running, refresh])

  /** 用户动作的失败直接展示，成功后回读持久状态。 */
  async function act(action: () => Promise<unknown>, success = '') {
    setBusy(true); setError(''); setNotice('')
    try { await action(); await refresh(); setNotice(success) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败') }
    finally { setBusy(false) }
  }

  /** 请求键只标识本次动作，重复资料版本由服务端判定为已有任务。 */
  async function start(kind: 'ingest' | 'lint', semantic = false) {
    // HTTP 部署不提供 randomUUID；使用支持 HTTP 的随机字节生成本次请求的唯一标识。
    const randomBytes = crypto.getRandomValues(new Uint8Array(16))
    const requestKey = Array.from(randomBytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
    const result = await wikiRequest<{ job: WikiJob }>(knowledgeBaseId, '/jobs', 'POST', { kind, semantic, documentIds: kind === 'ingest' ? selected : [], requestKey })
    if (result.job.status === 'failed') throw new Error(`该版本已有失败任务，请在任务列表重试：${result.job.error ?? ''}`)
    setSelected([])
  }

  /** 处理疑点必须填写理由，取消输入不发请求。 */
  function handleIssue(issue: WikiIssue, status: string) {
    const reason = window.prompt('请填写处理理由')?.trim()
    if (!reason) return
    void act(() => wikiRequest(knowledgeBaseId, `/issues/${encodeURIComponent(issue.id)}`, 'PATCH', { status, reason }))
  }

  // 派生知识页不出现在原始资料选择中；搜索不改变已选资料。
  const sourceDocuments = documents.filter((doc) => doc.kind !== 'wiki')
  const visibleSources = sourceDocuments.filter((doc) => doc.name.toLocaleLowerCase().includes(sourceQuery.toLocaleLowerCase()))
  const viewTitle = {
    read: '知识文库',
    sources: '原始资料',
    jobs: '整理任务',
    issues: '内容体检',
  }[view]
  const hasFilters = Boolean(query || pageType || freshness)

  return <section className="wiki-panel" aria-label="知识页维护">
    <aside className={`wiki-sidebar${directoryOpen ? ' is-open' : ''}`} aria-label="知识目录">
      <button className="wiki-mobile-toggle" aria-expanded={directoryOpen} onClick={() => setDirectoryOpen(!directoryOpen)}><AppIcon name="book" size={18} /><span>知识目录</span><AppIcon name={directoryOpen ? 'chevronUp' : 'chevronDown'} size={16} /></button>
      <div className="wiki-brand"><AppIcon name="book" size={22} /><span>知识文库<small>WIKI</small></span></div>
      <div className="wiki-library-picker">{libraryPicker}</div>
      <label className="wiki-search"><AppIcon name="search" size={16} /><input aria-label="搜索知识页" placeholder="搜索知识页…" value={query} onChange={(event) => {
          setQuery(event.target.value)
          setPageCursor('')
          setView('read')
          setPageId(null)
        }} /></label>
      <nav className="wiki-navigation" aria-label="文库导航">
        <button className={view === 'read' && !pageId && !pageType ? 'is-active' : ''} onClick={() => {
          setDirectoryOpen(false)
          setView('read')
          setPageId(null)
          setPageType('')
          setQuery('')
          setFreshness('')
          setPageCursor('')
        }}><AppIcon name="book" size={17} />文库首页<span>{overview?.counts.pages ?? '—'}</span></button>
        <button className={view === 'sources' ? 'is-active' : ''} onClick={() => {
          setView('sources')
          setDirectoryOpen(false)
        }}><AppIcon name="file" size={17} />原始资料<span>{sourceDocuments.length}</span></button>
        <button className={view === 'jobs' ? 'is-active' : ''} onClick={() => {
          setView('jobs')
          setDirectoryOpen(false)
        }}><AppIcon name="clock" size={17} />整理任务{running && <i className="wiki-live-dot" aria-label="有正在执行的任务" />}</button>
        <button className={view === 'issues' ? 'is-active' : ''} onClick={() => {
          setView('issues')
          setDirectoryOpen(false)
        }}><AppIcon name="check" size={17} />内容体检<span>{overview?.counts.issues ?? '—'}</span></button>
      </nav>
      <div className="wiki-directory-heading">知识目录<span>{pages.total}</span></div>
      <div className="wiki-directory-filters">
        <select aria-label="页面类型" value={pageType} onChange={(event) => {
          setPageType(event.target.value)
          setPageCursor('')
          setView('read')
          setPageId(null)
        }}><option value="">全部类型</option>{['topic', 'decision', 'source-summary', 'question'].map((value) => <option key={value} value={value}>{wikiStatusLabel(value)}</option>)}</select>
        <select aria-label="来源状态" value={freshness} onChange={(event) => {
          setFreshness(event.target.value)
          setPageCursor('')
          setView('read')
          setPageId(null)
        }}><option value="">全部状态</option>{['fresh', 'stale', 'missing-source'].map((value) => <option key={value} value={value}>{wikiStatusLabel(value)}</option>)}</select>
      </div>
      <nav className="wiki-page-tree" aria-label="知识页列表">
        {pages.items.map((page) => <button key={page.id} className={view === 'read' && pageId === page.id ? 'is-active' : ''} aria-current={view === 'read' && pageId === page.id ? 'page' : undefined} onClick={() => {
          setPageId(page.id)
          setView('read')
          setDirectoryOpen(false)
        }}><AppIcon name="file" size={15} /><span>{page.name}</span>{page.wiki.freshness !== 'fresh' && <AppIcon name="alert" size={13} />}</button>)}
        {!pages.items.length && <p className="wiki-directory-empty">{overview ? '知识页将在整理后出现在这里' : '正在读取目录…'}</p>}
      </nav>
      {(pageCursor || pages.nextCursor) && <div className="wiki-pagination">{pageCursor && <button onClick={() => setPageCursor('')}>回到首页</button>}{pages.nextCursor && <button onClick={() => setPageCursor(pages.nextCursor)}>下一页</button>}</div>}
      <div className="wiki-automation">
        <label className="wiki-switch"><span><AppIcon name="sparkles" size={16} />自动整理</span><input type="checkbox" role="switch" checked={overview?.enabled ?? false} disabled={busy || !overview} onChange={(event) => {
          const enabled = event.target.checked
          void act(() => wikiRequest(knowledgeBaseId, '', 'PUT', { enabled }), enabled ? '自动整理已开启；配置好第三方聊天模型后，新资料将自动整理。' : '自动整理已关闭。')
        }} /></label>
        <p>{wikiAutomationMessage(overview)}</p>
        <small>使用设置中保存的聊天模型</small>
      </div>
    </aside>

    <main className="wiki-main">
      <header className="wiki-topbar">
        <div className="wiki-breadcrumb"><span>{knowledgeBaseName}</span><AppIcon name="chevronRight" size={14} /><strong>{viewTitle}</strong></div>
        <div className="wiki-topbar-actions">
          <button className="wiki-icon-button" aria-label="刷新知识页" title="刷新" disabled={busy} onClick={() => void act(refresh)}><AppIcon name="refresh" size={16} /></button>
          <button className="wiki-export-button" disabled={busy} onClick={() => void act(() => downloadWiki(knowledgeBaseId))}><AppIcon name="download" size={16} /><span>导出 Markdown</span></button>
          <button className="wiki-primary" disabled={Boolean(uploadState)} onClick={() => fileInput.current?.click()}><AppIcon name="upload" size={16} />{uploadState ? `上传中 ${uploadState.completedFiles}/${uploadState.totalFiles}` : '上传资料'}</button>
          <input ref={fileInput} type="file" multiple accept=".txt,.md,.pdf,.csv,.xlsx" aria-label="上传原始资料" className="wiki-file-input" disabled={Boolean(uploadState)} onChange={(event) => {
          onUploadFiles(event.target.files)
          event.target.value = ''
          setView('sources')
        }} />
        </div>
      </header>
      {error && <p className="wiki-error wiki-feedback" role="alert">{error}</p>}
      {notice && <p className="wiki-notice wiki-feedback" role="status">{notice}</p>}
      {uploadState && <p className="wiki-feedback" role="status">正在上传：{uploadState.currentFileName}。{wikiAutomationMessage(overview)}</p>}

      <div className="wiki-content">
        {view === 'read' && (pageId ? <WikiPageViewer key={pageId} knowledgeBaseId={knowledgeBaseId} pageId={pageId} onClose={() => setPageId(null)} /> : <>
          <div className="wiki-home-heading"><span className="wiki-eyebrow">KNOWLEDGE WIKI</span><h2>{knowledgeBaseName} 的知识文库</h2><p>让零散资料成为相互关联、随时可查的知识。</p></div>
          <div className="wiki-overview-strip"><span><strong>{overview?.counts.pages ?? '—'}</strong> 知识页</span><span><strong>{sourceDocuments.length}</strong> 原始资料</span><span><strong>{overview?.pendingCount ?? '—'}</strong> 待整理</span><span><strong>{overview?.counts.stale ?? '—'}</strong> 来源失效</span></div>
          {!overview && !error && <p className="wiki-loading" role="status">正在读取知识文库…</p>}
          {overview && !overview.counts.pages && !hasFilters ? <section className="wiki-welcome">
            <div className="wiki-book-mark"><AppIcon name="book" size={42} /></div>
            <span className="wiki-eyebrow">从第一份资料开始</span>
            <h3>资料放进来，知识串起来。</h3>
            <p>上传文档，或选择库中已有的资料。整理后，主题、决策与资料摘要会出现在左侧目录中。</p>
            <div className="wiki-welcome-actions"><button className="wiki-primary" disabled={Boolean(uploadState)} onClick={() => fileInput.current?.click()}><AppIcon name="upload" size={16} />上传第一份资料</button><button onClick={() => setView('sources')}>从已有资料开始 <AppIcon name="chevronRight" size={16} /></button></div>
            <p className="wiki-welcome-hint">{wikiAutomationMessage(overview)}</p>
          </section> : overview && <section className="wiki-page-index">
            <div className="wiki-section-heading"><h3>{hasFilters ? '筛选结果' : '浏览知识页'}</h3><span>{pages.total} 页</span></div>
            {pages.items.map((page) => <button className="wiki-page-row" key={page.id} onClick={() => setPageId(page.id)}><AppIcon name="file" size={20} /><span><strong>{page.name}</strong><small>{wikiStatusLabel(page.wiki.pageType)} · {wikiStatusLabel(page.wiki.freshness)} · 修订 {page.wiki.revision}{page.wiki.indexedRevision !== page.wiki.revision && ' · 索引未完成'}</small></span><time>{formatWikiTime(page.wiki.updatedAt)}</time><AppIcon name="chevronRight" size={16} /></button>)}
            {!pages.items.length && <p className="wiki-muted">暂无符合条件的知识页，请调整搜索或筛选条件。</p>}
          </section>}
          <div className="wiki-guide"><div><span>01 / 收集</span><h4>上传原始资料</h4><p>支持 TXT、Markdown、PDF、CSV 和 XLSX，也可通过 MCP 上传文本资料。</p></div><div><span>02 / 整理</span><h4>形成关联知识</h4><p>默认开启自动整理，未配置第三方模型则跳过；已有资料可选择补整理，每批最多 20 份。</p></div><div><span>03 / 使用</span><h4>阅读与追溯</h4><p>查看知识页、历史修订和原始出处；通过内容体检发现过期来源与疑点。</p></div></div>
        </>)}

        {view === 'sources' && <>
          <div className="wiki-section-heading"><div><span className="wiki-eyebrow">SOURCES</span><h2>原始资料</h2><p>知识的出处都保留在这里。选择已有资料，开始整理成知识页。</p></div><span>{sourceDocuments.length} 份</span></div>
          <div className="wiki-source-help"><AppIcon name="info" size={20} /><div><strong>{wikiAutomationMessage(overview)}</strong><p>网页上传与 MCP 的 document.upload 共用入库流程。默认开启自动整理，复用设置中保存的第三方聊天模型；未配置时跳过，之后可选择资料补整理，不自动补跑历史资料。</p><p>上传成功不代表整理完成，请在“整理任务”中查看进度。MCP 文本上传需指定当前知识库 ID：<code>{knowledgeBaseId}</code>。</p></div></div>
          <div className="wiki-toolbar wiki-source-actions"><label className="wiki-search"><AppIcon name="search" size={16} /><input aria-label="搜索原始资料" placeholder="搜索资料名称…" value={sourceQuery} onChange={(event) => setSourceQuery(event.target.value)} /></label><span>已选 {selected.length}/20</span><button className="wiki-primary" disabled={busy || !overview?.enabled || selected.length === 0} onClick={() => void act(async () => {
          await start('ingest')
          setView('jobs')
        }, '整理任务已提交。')}><AppIcon name="sparkles" size={16} />整理所选资料</button></div>
          <div className="wiki-source-list">{visibleSources.map((doc) => <label key={doc.id} className={selected.includes(doc.id) ? 'is-selected' : ''}><input type="checkbox" checked={selected.includes(doc.id)} disabled={busy || (!selected.includes(doc.id) && selected.length >= 20)} onChange={(event) => setSelected((old) => event.target.checked ? [...old, doc.id] : old.filter((id) => id !== doc.id))} /><AppIcon name="file" size={18} /><span>{doc.name}</span></label>)}</div>
          {!visibleSources.length && <p className="wiki-muted">{sourceQuery ? '没有找到匹配的资料。' : '还没有原始资料，点击右上角“上传资料”开始。'}</p>}
        </>}

        {view === 'jobs' && <>
          <div className="wiki-section-heading"><div><span className="wiki-eyebrow">ACTIVITY</span><h2>整理任务</h2><p>查看资料整理、回答沉淀与内容体检的执行进度。</p></div>{running && <span className="wiki-status-pill">正在自动刷新</span>}</div>
          {overview?.jobs.map((job) => <article className="wiki-job" key={job.id}><div className="wiki-section-heading"><strong>{job.kind === 'lint' ? '内容体检' : job.kind === 'capture' ? '回答沉淀' : '资料整理'}</strong><span className="wiki-status-pill" data-status={job.status}>{wikiStatusLabel(job.status)}</span></div><span>{wikiStatusLabel(job.stage)} · 第 {job.attempt} 次执行 · {formatWikiTime(job.updatedAt)}</span><span>资料段落 {job.processedSegments}/{job.totalSegments} · 页面覆盖 {job.checkedPages}/{job.totalPages}{job.semanticStatus && ` · 语义检查：${wikiStatusLabel(job.semanticStatus)}`}</span>{job.error && <p className="wiki-error">{job.error}</p>}{job.status === 'failed' && job.attempt < 3 && <button disabled={busy || !overview.enabled} onClick={() => void act(() => wikiRequest(knowledgeBaseId, `/jobs/${encodeURIComponent(job.id)}/retry`, 'POST'))}>重试失败阶段</button>}</article>)}
          {overview && !overview.jobs.length && <div className="wiki-simple-empty"><AppIcon name="clock" size={32} /><h3>还没有整理任务</h3><p>开启自动整理后上传资料，或选择已有资料开始整理。</p><button onClick={() => setView('sources')}>选择已有资料</button></div>}
        </>}

        {view === 'issues' && <>
          <div className="wiki-section-heading"><div><span className="wiki-eyebrow">KNOWLEDGE HEALTH</span><h2>内容体检</h2><p>检查失效链接、过期来源和内容疑点，让知识保持可靠。</p></div></div>
          <div className="wiki-toolbar"><button disabled={busy || !overview?.enabled} onClick={() => void act(() => start('lint'), '结构检查已提交，请在整理任务中查看进度。')}>检查结构</button><button className="wiki-primary" disabled={busy || !overview?.enabled} onClick={() => void act(() => start('lint', true), '结构与语义检查已提交，请在整理任务中查看进度。')}>检查结构与语义</button><select aria-label="问题状态" value={issueStatus} onChange={(event) => {
          setIssueStatus(event.target.value)
          setIssueCursor('')
        }}>{['open', 'resolved', 'dismissed', ''].map((value) => <option key={value} value={value}>{value ? wikiStatusLabel(value) : '全部问题'}</option>)}</select></div>
          {!overview?.enabled && <p className="wiki-muted">请先开启自动整理，再运行内容体检。</p>}
          {issues.items.map((issue) => <article className="wiki-issue" key={issue.id}><strong>{wikiStatusLabel(issue.type)} · {wikiStatusLabel(issue.status)}</strong><p>{issue.reason}</p><div className="wiki-toolbar">{issue.pageIds.map((id) => <button key={id} onClick={() => {
          setPageId(id)
          setView('read')
        }}>查看关联页</button>)}{issue.status === 'open' ? <><button disabled={busy} onClick={() => handleIssue(issue, 'resolved')}>标记解决</button><button disabled={busy} onClick={() => handleIssue(issue, 'dismissed')}>忽略</button></> : <button disabled={busy} onClick={() => handleIssue(issue, 'open')}>重新打开</button>}</div></article>)}
          {issues.items.length === 0 && <div className="wiki-simple-empty"><AppIcon name="check" size={32} /><h3>暂无符合条件的问题</h3><p>体检覆盖范围以任务结果为准，尚未检查不代表没有问题。</p></div>}
          <div className="wiki-pagination">{issueCursor && <button onClick={() => setIssueCursor('')}>问题首页</button>}{issues.nextCursor && <button onClick={() => setIssueCursor(issues.nextCursor)}>下一页问题</button>}</div>
        </>}
      </div>
    </main>
  </section>
}

/** 区分开关意图与模型就绪状态；概览尚未加载时仅说明默认策略。 */
export function wikiAutomationMessage(overview: WikiOverview | null): string {
  if (!overview) {
    return '自动整理默认开启，未配置第三方聊天模型时跳过整理。'
  }
  if (!overview.enabled) {
    return '自动整理已关闭，上传资料仍会正常入库。'
  }
  if (!overview.autoCompileReady) {
    return '未配置第三方聊天模型，已跳过自动整理；原始资料正常入库。'
  }
  return '自动整理已开启，网页或 MCP 新上传、更新的资料会在入库后排队整理。'
}
