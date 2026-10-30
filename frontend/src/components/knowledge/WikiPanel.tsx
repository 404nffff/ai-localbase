import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import type { DocumentItem, KnowledgeBaseFileUploadState } from '../../App'
import { downloadWiki, wikiRequest, type WikiBatchResult, type WikiChange, type WikiIssue, type WikiJob, type WikiList, type WikiOverview, type WikiPage, type WikiPageContent, type WikiSourceContent, type WikiSourceRef } from '../../services/api'
import MarkdownRenderer from '../chat/LazyMarkdownRenderer'
import AppIcon from '../common/AppIcon'
import WikiPageEditor from './WikiPageEditor'
import '../../styles/wiki.css'

/** 整理和页面状态的中文说明，保留未知状态便于诊断新版本。 */
export function wikiStatusLabel(value: string): string {
  const labels: Record<string, string> = { queued: '排队中', running: '整理中', succeeded: '已完成', failed: '失败', partial: '部分完成', needs_split: '超限待恢复', canceled: '已取消', fresh: '有效', stale: '来源已更新', 'missing-source': '来源已删除', topic: '主题', decision: '决策', 'source-summary': '资料摘要', question: '待核实', open: '待处理', resolved: '已解决', dismissed: '已忽略', completed: '完成', unavailable: '未完成', not_requested: '未请求' }
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
export function WikiPageViewer({ knowledgeBaseId, pageId, initialRevision = 0, showHistory = false, onClose, onSaved }: { knowledgeBaseId: string; pageId: string; initialRevision?: number; /** 独立 Wiki 展示历史和编辑入口，聊天引用保持阅读模式。 */ showHistory?: boolean; onClose: () => void; /** 保存后刷新宿主目录和修订列表。 */ onSaved?: () => void }) {
  const [page, setPage] = useState<WikiPageContent | null>(null)
  const [selectedPage, setSelectedPage] = useState(pageId)
  const [revision, setRevision] = useState(initialRevision)
  const [source, setSource] = useState<WikiSourceContent | null>(null)
  const [sourceRef, setSourceRef] = useState<WikiSourceRef | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState(false)
  const [saveNotice, setSaveNotice] = useState('')
  const [readVersion, setReadVersion] = useState(0)
  const generation = useRef(0)
  const sourceElement = useRef<HTMLElement>(null)
  useEffect(() => {
    if (source) {
      sourceElement.current?.scrollIntoView({ block: 'nearest' })
      sourceElement.current?.focus({ preventScroll: true })
    }
  }, [source])

  useEffect(() => {
    const controller = new AbortController()
    generation.current += 1
    setPage(null); setSource(null); setSourceRef(null); setError(''); setBusy(false)
    void wikiRequest<WikiPageContent>(knowledgeBaseId, `/pages/${encodeURIComponent(selectedPage)}?revision=${revision}`, 'GET', undefined, controller.signal)
      .then((result) => { if (!controller.signal.aborted) setPage(result) }).catch((reason: Error) => { if (!controller.signal.aborted) setError(reason.message) })
    return () => { controller.abort(); generation.current += 1 }
  }, [knowledgeBaseId, selectedPage, revision, readVersion])

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

  return <section className={`wiki-reader${showHistory ? ' wiki-reader-with-history' : ''}`} aria-label="知识页详情">
    <div className="wiki-reader-body">
    <div className="wiki-toolbar"><h3>{page?.title ?? '读取知识页…'}</h3>{showHistory && <button type="button" className="wiki-primary" disabled={!page || busy} onClick={() => setEditing(true)}><AppIcon name="pencil" size={16} />{revision ? '编辑当前版本' : '编辑内容'}</button>}<button type="button" onClick={onClose}>关闭详情</button></div>
    {saveNotice && <p role="status" className="wiki-save-notice">{saveNotice}</p>}
    {editing && <WikiPageEditor knowledgeBaseId={knowledgeBaseId} pageId={selectedPage} onClose={() => setEditing(false)} onSaved={(result) => {
      setEditing(false)
      setRevision(0)
      setReadVersion((version) => version + 1)
      setSaveNotice(result.indexed ? `修改已保存为修订 ${result.revision}，索引已更新。` : `修改已保存为修订 ${result.revision}；索引尚未完成：${result.indexError ?? '请稍后重建索引'}。无需重复保存正文。`)
      onSaved?.()
    }} />}
    {error && <p role="alert" className="wiki-error">{error}</p>}
    {page && <>
      <div className="wiki-toolbar"><span>{wikiStatusLabel(page.metadata.freshness)}</span><span>正在阅读修订 {page.revision}</span>{!showHistory && <label>历史修订 <select value={revision} onChange={(event) => setRevision(Number(event.target.value))}>
        <option value={0}>当前版本</option>{Array.from({ length: page.metadata.revision }, (_, index) => <option key={index + 1} value={index + 1}>修订 {index + 1}</option>)}
      </select></label>}<span>共 {page.totalChars} 字符</span></div>
      <MarkdownRenderer content={page.markdown} preserveSource />
      {page.nextOffset > 0 && <button disabled={busy} onClick={() => void morePage()}>加载后续正文</button>}
      <h3>原始来源</h3>
      {!page.sourceRefs?.length && <p>本页没有已验证的原始来源，请按待核实内容使用。</p>}
      {(page.sourceRefs ?? []).map((ref, index) => <div className="wiki-evidence" key={`${ref.documentId}-${index}`}><blockquote>{ref.quote}</blockquote><button disabled={busy || page.missingSources.includes(ref.documentId)} onClick={() => void readSource(ref)}>{page.missingSources.includes(ref.documentId) ? '来源已删除' : `查看来源 ${ref.documentId}`}</button><small>版本 {ref.checksum.slice(0, 12)}</small></div>)}
      {source && <section className="wiki-source" ref={sourceElement} tabIndex={-1} aria-label="原始来源正文"><h3>{source.name} · 原始版本</h3><pre>{source.content}</pre>{source.nextOffset > 0 && sourceRef && <button disabled={busy} onClick={() => void readSource(sourceRef, source.nextOffset)}>加载后续来源</button>}</section>}
      {(page.links ?? []).length > 0 && <div className="wiki-toolbar"><span>关联知识页</span>{page.links.map((id) => <button key={id} onClick={() => { setSelectedPage(id); setRevision(0) }}>{id}</button>)}</div>}
    </>}
    </div>
    {showHistory && page && <aside className="wiki-revision-panel" aria-label="更新历史">
      <h3><AppIcon name="clock" size={16} />更新历史</h3>
      <p>点击修订，查看当时的整理正文。</p>
      <button className={revision === 0 ? 'is-active' : ''} aria-pressed={revision === 0} onClick={() => setRevision(0)}>当前版本 · 修订 {page.metadata.revision}</button>
      <ol>{[...(page.metadata.history ?? [])].sort((a, b) => b.revision - a.revision).map((item) => <li key={item.revision}>
        <button className={revision === item.revision ? 'is-active' : ''} aria-pressed={revision === item.revision} onClick={() => setRevision(item.revision)}>
          <strong>修订 {item.revision}</strong><time dateTime={item.createdAt}>{formatWikiTime(item.createdAt)}</time><span>{item.title}</span>
        </button>
      </li>)}</ol>
      {!page.metadata.history?.length && <p>暂无可用的历史修订记录。</p>}
    </aside>}
  </section>
}

/** 单库阅读工作区的宿主入口，上传沿用知识库原有处理与进度。 */
interface WikiPanelProps {
  /** 将当前阅读位置交给宿主写入 URL，刷新后恢复相同知识页。 */
  onNavigate?: (pageId: string | null, revision: number) => void
  /** 从全库历史进入时定位发布时的页面和修订。 */
  initialPageId?: string
  initialRevision?: number
  /** 打开跨所有知识库的整理历史。 */
  onOpenHistory?: () => void
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
  /** 打开统一整理进度页，定位当前库并查看完整任务历史。 */
  onOpenProgress?: () => void
}

/**
 * 以知识目录与正文为主，资料、任务和体检在各自视图中维护。
 * 宿主负责当前库选择、原始文件上传及上传失败反馈；本组件回读 Wiki 状态，
 * 展示整理错误并轮询已运行任务。历史资料仍需显式选择，切换库由宿主 key 重建状态。
 */
export default function WikiPanel({ knowledgeBaseId, knowledgeBaseName, documents, libraryPicker, onUploadFiles, uploadState, onOpenProgress, onOpenHistory, initialPageId, initialRevision = 0, onNavigate }: WikiPanelProps) {
  const [overview, setOverview] = useState<WikiOverview | null>(null)
  const [pages, setPages] = useState<WikiList<WikiPage>>({ items: [], total: 0, nextCursor: '' })
  const [issues, setIssues] = useState<WikiList<WikiIssue>>({ items: [], total: 0, nextCursor: '' })
  const [selected, setSelected] = useState<string[]>([])
  const [batchResult, setBatchResult] = useState<WikiBatchResult | null>(null)
  const [pageId, setPageId] = useState<string | null>(initialPageId ?? null)
  const [pageRevision, setPageRevision] = useState(initialRevision)
  const [changes, setChanges] = useState<WikiList<WikiChange>>({ items: [], total: 0, nextCursor: '' })
  const [changeOffset, setChangeOffset] = useState(0)
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
      wikiRequest<WikiList<WikiChange>>(knowledgeBaseId, `/changes?offset=${changeOffset}&limit=6`),
    ])
    if (current !== generation.current) return
    setOverview(results[0]); setPages(results[1]); setIssues(results[2])
    setChanges(results[3])
  }, [knowledgeBaseId, query, pageType, freshness, pageCursor, issueStatus, issueCursor, changeOffset])

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

  const hasFilters = Boolean(query || pageType || freshness)

  /** 目录默认读最新正文；整理记录固定到当时发布的修订，切换时关闭手机目录。 */
  function openPage(id: string | null, revision = 0) {
    setPageId(id)
    setPageRevision(revision)
    setView('read')
    setDirectoryOpen(false)
    onNavigate?.(id, revision)
  }

  return <section className={`wiki-panel${view !== 'read' || !pages.items.length ? ' wiki-panel-wide' : ''}`} aria-label="知识页维护">
    {/* 工作区操作集中放在顶部，左栏只承载知识目录，避免列表被表单和说明挤压。 */}
    <header className="wiki-workspace-header">
      <div className="wiki-brand"><AppIcon name="book" size={22} /><span>知识文库<small>WIKI</small></span></div>
      <div className="wiki-library-picker">{libraryPicker}</div>
      <div className="wiki-topbar-actions">
        {onOpenHistory && <button onClick={onOpenHistory}><AppIcon name="clock" size={16} />全部整理历史</button>}
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
    <div className="wiki-workspace-tools">
      <nav className="wiki-navigation" aria-label="文库导航">
        <button className={view === 'read' && !pageId && !pageType ? 'is-active' : ''} onClick={() => {
          openPage(null)
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
      <label className="wiki-search"><AppIcon name="search" size={16} /><input aria-label="搜索知识页" placeholder="搜索知识页…" value={query} onChange={(event) => {
          setQuery(event.target.value)
          setPageCursor('')
          openPage(null)
        }} /></label>
      <details className="wiki-workspace-options">
        <summary>筛选与整理设置{hasFilters && <span> · 已筛选</span>}</summary>
        <div className="wiki-workspace-options-panel">
          <div className="wiki-directory-filters">
            <select aria-label="页面类型" value={pageType} onChange={(event) => {
              setPageType(event.target.value)
              setPageCursor('')
              openPage(null)
            }}><option value="">全部类型</option>{['topic', 'decision', 'source-summary', 'question'].map((value) => <option key={value} value={value}>{wikiStatusLabel(value)}</option>)}</select>
            <select aria-label="来源状态" value={freshness} onChange={(event) => {
              setFreshness(event.target.value)
              setPageCursor('')
              openPage(null)
            }}><option value="">全部状态</option>{['fresh', 'stale', 'missing-source'].map((value) => <option key={value} value={value}>{wikiStatusLabel(value)}</option>)}</select>
          </div>
          <div className="wiki-automation">
            <label className="wiki-switch"><span><AppIcon name="sparkles" size={16} />自动整理</span><input type="checkbox" role="switch" checked={overview?.enabled ?? false} disabled={busy || !overview} onChange={(event) => {
              const enabled = event.target.checked
              void act(() => wikiRequest(knowledgeBaseId, '', 'PUT', { enabled }), enabled ? '自动整理已开启；配置好第三方聊天模型后，新资料将自动整理。' : '自动整理已关闭。')
            }} /></label>
            <details><summary>{overview?.enabled ? '开启后如何整理' : '查看整理说明'}</summary><p>{wikiAutomationMessage(overview)}</p></details>
          </div>
        </div>
      </details>
    </div>
    <aside className={`wiki-sidebar${directoryOpen ? ' is-open' : ''}`} aria-label="知识目录">
      <button className="wiki-mobile-toggle" aria-expanded={directoryOpen} onClick={() => setDirectoryOpen(!directoryOpen)}><AppIcon name="book" size={18} /><span>知识目录</span><AppIcon name={directoryOpen ? 'chevronUp' : 'chevronDown'} size={16} /></button>
      <div className="wiki-directory-heading">知识目录<span>{pages.total} 页</span></div>
      <nav className="wiki-page-tree" aria-label="知识页列表">
        {pages.items.map((page) => <button key={page.id} className={view === 'read' && pageId === page.id ? 'is-active' : ''} aria-current={view === 'read' && pageId === page.id ? 'page' : undefined} onClick={() => {
          openPage(page.id)
        }}><AppIcon name="file" size={15} /><span>{page.name}</span>{page.wiki.freshness !== 'fresh' && <AppIcon name="alert" size={13} />}</button>)}
        {!pages.items.length && <p className="wiki-directory-empty">{overview ? (hasFilters ? '没有符合筛选条件的知识页' : '知识页将在整理后出现在这里') : '正在读取目录…'}</p>}
      </nav>
      {(pageCursor || pages.nextCursor) && <div className="wiki-pagination">{pageCursor && <button onClick={() => setPageCursor('')}>回到首页</button>}{pages.nextCursor && <button onClick={() => setPageCursor(pages.nextCursor)}>下一页</button>}</div>}
    </aside>

    <div className="wiki-main">
      {view === 'read' && changes.total > 0 && <details className="wiki-recent-changes">
        <summary><span><AppIcon name="sparkles" size={17} />整理后的文档记录</span><small>{changes.total} 次发布 · 点击查看整理内容</small></summary>
        <div className="wiki-change-list">{changes.items.map((item) => <button key={`${item.pageId}-${item.revision}`} className={pageId === item.pageId && pageRevision === item.revision ? 'is-active' : ''} onClick={() => openPage(item.pageId, item.revision)}>
          <strong>{item.title}</strong><span>修订 {item.revision} · {item.sourceCount} 份来源</span><time dateTime={item.createdAt}>{formatWikiTime(item.createdAt)}</time>
        </button>)}</div>
        {!changes.items.length && <p>{overview ? '暂无已发布的整理内容。可从上方选择原始资料开始整理，或查看整理任务。' : '正在读取整理记录…'}</p>}
        {(changeOffset > 0 || changes.nextCursor) && <nav className="wiki-change-pagination" aria-label="整理记录分页"><span>第 {Math.floor(changeOffset / 6) + 1} 页</span><button disabled={changeOffset === 0} onClick={() => setChangeOffset(Math.max(0, changeOffset - 6))}>较新记录</button><button disabled={!changes.nextCursor} onClick={() => setChangeOffset(Number(changes.nextCursor))}>更早记录</button></nav>}
      </details>}
      {error && <p className="wiki-error wiki-feedback" role="alert">{error}</p>}
      {notice && <p className="wiki-notice wiki-feedback" role="status">{notice}</p>}
      {batchResult && <div className="wiki-feedback" role="status"><strong>批量处理结果：{batchResult.items.length} 份资料</strong><ul>{batchResult.items.map((item) => <li key={item.documentId}>{item.name}：{item.error || wikiStatusLabel(item.status)}</li>)}</ul><p>排队中的任务尚未完成，请在整理任务中查看结果。</p></div>}
      {uploadState && <p className="wiki-feedback" role="status">正在上传：{uploadState.currentFileName}。{wikiAutomationMessage(overview)}</p>}

      <div className="wiki-content">
        {view === 'read' && (pageId ? <WikiPageViewer key={`${pageId}-${pageRevision}`} knowledgeBaseId={knowledgeBaseId} pageId={pageId} initialRevision={pageRevision} showHistory onSaved={() => void act(refresh)} onClose={() => openPage(null)} /> : <>
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
            {pages.items.map((page) => <button className="wiki-page-row" key={page.id} onClick={() => openPage(page.id)}><AppIcon name="file" size={20} /><span><strong>{page.name}</strong><small>{wikiStatusLabel(page.wiki.pageType)} · {wikiStatusLabel(page.wiki.freshness)} · 修订 {page.wiki.revision}{page.wiki.indexedRevision !== page.wiki.revision && ' · 索引未完成'}</small></span><time>{formatWikiTime(page.wiki.updatedAt)}</time><AppIcon name="chevronRight" size={16} /></button>)}
            {!pages.items.length && <p className="wiki-muted">暂无符合条件的知识页，请调整搜索或筛选条件。</p>}
          </section>}
          <div className="wiki-guide"><div><span>01 / 收集</span><h3>上传原始资料</h3><p>支持 TXT、Markdown、PDF、CSV 和 XLSX，也可通过 MCP 上传文本资料。</p></div><div><span>02 / 整理</span><h3>形成关联知识</h3><p>默认开启自动整理，未配置第三方模型则跳过；已有资料可批量选择补整理，每份资料独立排队。</p></div><div><span>03 / 使用</span><h3>阅读与追溯</h3><p>查看知识页、历史修订和原始出处；通过内容体检发现过期来源与疑点。</p></div></div>
        </>)}

        {view === 'sources' && <>
          <div className="wiki-section-heading"><div><span className="wiki-eyebrow">SOURCES</span><h2>原始资料</h2><p>知识的出处都保留在这里。选择已有资料，开始整理成知识页。</p></div><span>{sourceDocuments.length} 份</span></div>
          <div className="wiki-source-help"><AppIcon name="info" size={20} /><div><strong>{wikiAutomationMessage(overview)}</strong><p>选择资料后点击“整理所选资料”，完成情况可在“整理任务”查看。</p><details><summary>上传与接入说明</summary><p>历史资料需手动补整理；上传成功不代表整理完成。通过 MCP 上传文本时指定知识库 ID：<code>{knowledgeBaseId}</code>。</p></details></div></div>
          <div className="wiki-toolbar wiki-source-actions"><label className="wiki-search"><AppIcon name="search" size={16} /><input aria-label="搜索原始资料" placeholder="搜索资料名称…" value={sourceQuery} onChange={(event) => setSourceQuery(event.target.value)} /></label><span>已选 {selected.length} 份</span><button disabled={busy || !visibleSources.length} onClick={() => {
            // 全选只补充当前筛选结果，不丢弃用户在其他筛选下已选的资料。
            setSelected((current) => [...new Set([...current, ...visibleSources.map((doc) => doc.id)])])
          }}>全选筛选结果</button><button disabled={busy || !selected.length} onClick={() => setSelected([])}>取消选择</button><button className="wiki-primary" disabled={busy || !overview?.enabled || !overview.autoCompileReady || selected.length === 0} onClick={() => void act(() => startBatch(selected))}><AppIcon name="sparkles" size={16} />整理所选资料</button><button disabled={busy || !overview?.enabled || !overview.autoCompileReady || !overview.pendingCount} onClick={() => void act(() => startBatch([]))}>一键整理本库待处理资料</button></div>
          <div className="wiki-source-list">{visibleSources.map((doc) => {
            const status = overview?.sourceStates?.[doc.id]
            let label = '状态读取中'
            if (status === 'succeeded') {
              label = '已整理'
            } else if (status === 'failed') {
              label = '待整理 · 上次失败'
            } else if (status === 'pending') {
              label = '待整理'
            } else if (status) {
              label = wikiStatusLabel(status)
            }
            return <label key={doc.id} className={selected.includes(doc.id) ? 'is-selected' : ''}><input type="checkbox" checked={selected.includes(doc.id)} disabled={busy} onChange={(event) => setSelected((old) => event.target.checked ? [...old, doc.id] : old.filter((id) => id !== doc.id))} /><AppIcon name="file" size={18} /><span>{doc.name}</span><small className="wiki-status-pill" data-status={status}>{label}</small></label>
          })}</div>
          {!visibleSources.length && <p className="wiki-muted">{sourceQuery ? '没有找到匹配的资料。' : '还没有原始资料，点击右上角“上传资料”开始。'}</p>}
        </>}

        {view === 'jobs' && <>
          <div className="wiki-section-heading"><div><span className="wiki-eyebrow">ACTIVITY</span><h2>整理任务</h2><p>最近 50 个任务。查看全库进度、筛选失败任务和完整历史，请打开整理总进度。</p></div>{onOpenProgress && <button onClick={onOpenProgress}><AppIcon name="clock" size={17} />整理总进度<AppIcon name="chevronRight" size={16} /></button>}</div>
          {running && <p className="wiki-muted">正在自动刷新任务进度</p>}
          {overview && <WikiJobList jobs={overview.jobs} enabled={overview.enabled} busy={busy} onRetry={(job) => void act(() => wikiRequest(knowledgeBaseId, `/jobs/${encodeURIComponent(job.id)}/retry`, 'POST'))} />}
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
          openPage(id)
        }}>查看关联页</button>)}{issue.status === 'open' ? <><button disabled={busy} onClick={() => handleIssue(issue, 'resolved')}>标记解决</button><button disabled={busy} onClick={() => handleIssue(issue, 'dismissed')}>忽略</button></> : <button disabled={busy} onClick={() => handleIssue(issue, 'open')}>重新打开</button>}</div></article>)}
          {issues.items.length === 0 && <div className="wiki-simple-empty"><AppIcon name="check" size={32} /><h3>暂无符合条件的问题</h3><p>体检覆盖范围以任务结果为准，尚未检查不代表没有问题。</p></div>}
          <div className="wiki-pagination">{issueCursor && <button onClick={() => setIssueCursor('')}>问题首页</button>}{issues.nextCursor && <button onClick={() => setIssueCursor(issues.nextCursor)}>下一页问题</button>}</div>
        </>}
      </div>
    </div>
  </section>

  /** 每份资料独立建任务；保留逐项结果，部分提交失败也能查看已排队项。 */
  async function startBatch(documentIds: string[]) {
    setBatchResult(null)
    const result = await wikiRequest<WikiBatchResult>(knowledgeBaseId, '/ingest-batch', 'POST', { documentIds })
    setBatchResult(result)
    setSelected(result.items.filter((item) => item.error).map((item) => item.documentId))
  }

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

/**
 * 展示真实任务阶段、处理覆盖与失败恢复入口，供单库文库和总进度页使用。
 * 分段/页面覆盖只代表读取进度，不将其当作整个任务完成率；重试交给宿主调用原接口。
 */
export function WikiJobList({ jobs, enabled, busy, onRetry }: {
  /** 当前页的任务，排序与分页由宿主负责。 */
  jobs: WikiJob[]
  /** 库关闭时不允许重试，但仍保留历史结果。 */
  enabled: boolean
  /** 正在提交或回读时禁止重复重试。 */
  busy: boolean
  /** 对失败或暂停任务显式重试，累计执行次数不限制用户恢复。 */
  onRetry: (job: WikiJob) => void
}) {
  return <div className="wiki-job-list">{jobs.map((job) => {
    const steps = job.kind === 'lint'
      ? ['structure', 'semantic', 'finalize']
      : ['read_sources', 'extract', 'plan', 'validate', 'publish', 'index', 'finalize']
    const stageIndex = steps.indexOf(job.stage)
    // 真实预算由服务端按来源段数计算，旧任务恢复时会重新核算，不以固定32次阻断长文。
    const canRetry = (job.status === 'failed' || job.status === 'needs_split') && enabled
    let taskTitle = '资料整理'
    if (job.kind === 'lint') {
      taskTitle = '内容体检'
    } else if (job.kind === 'capture') {
      taskTitle = '回答沉淀'
    }
    return <article className="wiki-task-card" key={job.id}>
      <header className="wiki-task-heading">
        <div className="wiki-task-title"><span className="wiki-task-icon"><AppIcon name={job.kind === 'lint' ? 'check' : 'file'} size={20} /></span><div><h3>{job.sourceNames?.length ? job.sourceNames.join('、') : taskTitle}</h3><small>{taskTitle} · {job.id}</small></div></div>
        <span className="wiki-status-pill" data-status={job.status}>{wikiStatusLabel(job.status)}</span>
      </header>
      <div className="wiki-task-stage"><strong>{wikiStatusLabel(job.stage)}</strong><span>{job.attempt > 0 ? `累计执行 ${job.attempt} 次` : '等待执行'}</span></div>
      <ol className="wiki-task-steps" aria-label="整理阶段">{steps.map((step, index) => {
        // 成功任务全部标记完成，其他任务按服务端当前阶段区分已过、当前与等待。
        let stepState = 'waiting'
        if (job.status === 'succeeded' || index < stageIndex) {
          stepState = 'done'
        } else if (index === stageIndex) {
          stepState = 'current'
        }
        return <li key={step} data-state={stepState} aria-current={step === job.stage ? 'step' : undefined}><span />{wikiStatusLabel(step)}</li>
      })}</ol>
      <div className="wiki-task-coverage">
        {job.totalSegments > 0 && <label>资料段落 <strong>{job.processedSegments} / {job.totalSegments}</strong><progress aria-label="资料段落处理进度" value={job.processedSegments} max={job.totalSegments} /></label>}
        {job.totalPages > 0 && <label>页面检查 <strong>{job.checkedPages} / {job.totalPages}</strong><progress aria-label="页面检查进度" value={job.checkedPages} max={job.totalPages} /></label>}
      </div>
      {job.semanticStatus && <p className="wiki-task-note">语义检查：{wikiStatusLabel(job.semanticStatus)}</p>}
      {job.error && <div className="wiki-task-error"><AppIcon name="alert" size={18} /><div><strong>失败原因</strong><p>{wikiFailureMessage(job.error)}</p>{wikiFailureMessage(job.error) !== job.error && <details><summary>原始错误</summary><p>{job.error}</p></details>}</div></div>}
      {job.status === 'partial' && <p className="wiki-task-note">只完成了部分检查，请查看覆盖范围；补充条件后重新发起体检。</p>}
      {job.status === 'needs_split' && <p className="wiki-task-note">旧任务因处理预算暂停。重试后系统会自动拆批，较长内容整理为关联专题页，无需手动拆分原始资料。</p>}
      {(job.modelCalls ?? 0) > 0 && <p className="wiki-task-note">累计执行 {job.modelCalls} 次模型调用。长文按段处理，重试保留已完成的提取结果。</p>}
      {(job.status === 'failed' || job.status === 'needs_split') && !enabled && <p className="wiki-task-note">本库自动整理已关闭，开启后可重试。</p>}
      <footer className="wiki-task-footer"><time dateTime={job.updatedAt}>更新于 {formatWikiTime(job.updatedAt)}</time>{canRetry && <button disabled={busy} onClick={() => onRetry(job)}><AppIcon name="refresh" size={15} />重试失败阶段</button>}</footer>
    </article>
  })}</div>
}

/** 历史英文错误补充可操作说明，详细原始原因仍保留供排查。 */
export function wikiFailureMessage(error: string): string {
  if (error.includes('failed to read model response')) {
    return '读取模型响应时中断，这条记录未保存具体原因。可重试失败阶段，已完成的提取结果会保留。'
  }
  if (error.includes('combined context exceeds compilation budget')) {
    return '旧任务的合并内容超过单次预算。现在已支持分批综合，可直接重试。'
  }
  if (error.includes('without reading its baseline')) {
    return '模型尝试修改未读取的知识页。现在会补读完整基线再生成，可重试该任务。'
  }
  if (error.includes('quote does not belong to source segment')) {
    return '模型引用与原文不一致。现在支持按原文行号生成准确引用，可重试该任务。'
  }
  if (error.startsWith('configured chat model failed: ')) {
    return `模型调用失败：${error.slice('configured chat model failed: '.length)}`
  }
  return error
}
