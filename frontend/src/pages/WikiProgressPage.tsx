import { useEffect, useMemo, useRef, useState } from 'react'
import { fetchWikiProgress, updateWikiQueue, wikiRequest, type WikiBatchResult, type WikiJob, type WikiProgressResponse, type WikiRetryBatchResult } from '../services/api'
import AppIcon from '../components/common/AppIcon'
import { formatWikiTime, wikiStatusLabel, WikiJobList } from '../components/knowledge/WikiPanel'
import '../styles/wiki-progress.css'

// 异常按任务状态统计；待整理资料另按来源版本统计，两者不能相加。
const attentionStatuses = ['failed', 'partial', 'needs_split'] as const

/**
 * 全库整理工作台：全量统计、按库钻取、状态筛选与分页，复用已有失败重试接口。
 * 轮询串行执行，离开页面或变更筛选时取消旧读取；读取失败停止轮询，保留上次快照并明确标记。
 */
export default function WikiProgressPage({ initialKnowledgeBaseId = '', onClose }: {
  /** 从文库进入时定位该库，从主导航进入时显示全部库。 */
  initialKnowledgeBaseId?: string
  /** 返回会话工作区。 */
  onClose: () => void
}) {
  const [selectedId, setSelectedId] = useState(initialKnowledgeBaseId)
  const [status, setStatus] = useState('')
  const [offset, setOffset] = useState(0)
  const [query, setQuery] = useState('')
  const [libraryFilter, setLibraryFilter] = useState('all')
  const [data, setData] = useState<WikiProgressResponse | null>(null)
  const [error, setError] = useState('')
  const [actionError, setActionError] = useState('')
  const [notice, setNotice] = useState('')
  const [batchResult, setBatchResult] = useState<WikiRetryBatchResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [retrying, setRetrying] = useState(false)
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [refreshVersion, setRefreshVersion] = useState(0)
  const [updatedAt, setUpdatedAt] = useState('')
  const [queueBusy, setQueueBusy] = useState(false)
  const [queueError, setQueueError] = useState('')
  // null代表尚未编辑，空字符串保留用户清空输入的动作。
  const [concurrencyDraft, setConcurrencyDraft] = useState<string | null>(null)
  const [queueNotice, setQueueNotice] = useState('')
  // 点击互斥锁与保存版本号用于隔离并行操作和旧轮询回包。
  const queueLock = useRef(false)
  const queueVersion = useRef(0)
  // 保存回执独立于筛选快照，切库期间仍能防止旧轮询覆盖新设置。
  const confirmedQueue = useRef<WikiProgressResponse['queue'] | null>(null)
  const retryLock = useRef(false)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    setData(null)
    setLoading(true)
    setError('')
    const params = new URLSearchParams({ knowledgeBaseId: selectedId, status, offset: String(offset), limit: '20' })

    /** 每轮完成后再安排下一轮，错误时保留快照且停止自动读取。 */
    async function refresh() {
      try {
        const queueRevision = queueVersion.current
        const result = await fetchWikiProgress(params, controller.signal)
        if (controller.signal.aborted) {
          return
        }
        // 保存期间的旧轮询只能刷新任务数据，不能覆盖较新的队列设置。
        setData((previous) => {
          if (queueLock.current || queueVersion.current !== queueRevision) {
            const queue = confirmedQueue.current ?? previous?.queue
            if (queue) {
              return { ...result, queue }
            }
          }
          return result
        })
        setUpdatedAt(new Date().toISOString())
        setError('')
        // 新任务或重试会改变筛选总数，空的末页退回有效的最后一页。
        if (offset > 0 && offset >= result.jobs.total) {
          setOffset(Math.max(0, Math.ceil(result.jobs.total / 20) - 1) * 20)
        }
        if (autoRefresh) {
          timer = setTimeout(() => void refresh(), 5000)
        }
      } catch (reason) {
        if (!controller.signal.aborted) {
          setError(reason instanceof Error ? reason.message : '读取整理进度失败')
        }
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
        }
      }
    }

    void refresh()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [selectedId, status, offset, autoRefresh, refreshVersion])

  const totals = useMemo(() => {
    const result = { total: 0, succeeded: 0, running: 0, queued: 0, attention: 0, pending: 0, failed: 0, partial: 0, needs_split: 0 }
    for (const library of data?.items ?? []) {
      result.total += library.total
      result.succeeded += library.counts.succeeded ?? 0
      result.running += library.counts.running ?? 0
      result.queued += library.counts.queued ?? 0
      for (const value of attentionStatuses) {
        result[value] += library.counts[value] ?? 0
        result.attention += library.counts[value] ?? 0
      }
      result.pending += library.pendingCount
    }
    return result
  }, [data])

  const libraries = useMemo(() => {
    const visible = []
    for (const library of data?.items ?? []) {
      if (!library.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())) {
        continue
      }
      if (libraryFilter === 'active' && !(library.counts.running || library.counts.queued)) {
        continue
      }
      if (libraryFilter === 'attention' && !attentionStatuses.some((value) => library.counts[value])) {
        continue
      }
      if (libraryFilter === 'pending' && !library.pendingCount) {
        continue
      }
      if (attentionStatuses.some((value) => value === libraryFilter) && !library.counts[libraryFilter]) {
        continue
      }
      visible.push(library)
    }
    // 先展示执行中的库，再展示异常任务或待整理资料所在库，同组保留服务端稳定顺序。
    visible.sort((left, right) => {
      const leftRank = Number(Boolean(left.counts.running || left.counts.queued)) * 2 + Number(Boolean(left.pendingCount || left.counts.failed || left.counts.partial || left.counts.needs_split))
      const rightRank = Number(Boolean(right.counts.running || right.counts.queued)) * 2 + Number(Boolean(right.pendingCount || right.counts.failed || right.counts.partial || right.counts.needs_split))
      return rightRank - leftRank
    })
    return visible
  }, [data, query, libraryFilter])

  const selectedLibrary = data?.items.find((library) => library.knowledgeBaseId === selectedId)
  // 按当前可见范围提交，搜索与状态筛选不会悄悄扩大成全站操作。
  const pendingLibraries = (selectedId ? data?.items.filter((library) => library.knowledgeBaseId === selectedId) ?? [] : libraries)
    .filter((library) => library.enabled && library.pendingCount > 0)
  const pendingCount = pendingLibraries.reduce((count, library) => count + library.pendingCount, 0)
  const pendingControls = <div className="wiki-pending-actions">
    <span>{selectedId ? '当前知识库' : `当前筛选 · ${pendingLibraries.length} 个可处理知识库`}<strong>{pendingCount} 份待整理</strong></span>
    <button className="wiki-pending-submit" title="逐份排队，已完成和执行中的资料会跳过；失败任务保留进度后重试。" disabled={retrying || loading || Boolean(error) || !data?.autoCompileReady || pendingCount === 0} onClick={() => void processPending()}><AppIcon name={retrying ? 'loader' : 'sparkles'} size={16} />{retrying ? '正在提交…' : '整理待处理资料'}</button>
  </div>
  const completion = totals.total > 0 ? Math.floor(totals.succeeded / totals.total * 100) : 0
  // 失败提示优先于用户选择的刷新模式，避免暂停后仍显示自动更新。
  let refreshLabel = '手动刷新'
  if (error) {
    refreshLabel = '刷新已暂停'
  } else if (autoRefresh) {
    refreshLabel = '每 5 秒自动更新'
  }

  /** 切换范围时重置分页和操作反馈，避免上一库错误留在新库。 */
  function selectLibrary(id: string, taskStatus = '') {
    setSelectedId(id)
    setStatus(taskStatus)
    setOffset(0)
    setActionError('')
    setNotice('')
    setBatchResult(null)
  }

  /** 全站统计入口清空旧搜索和库范围，保证点击后的列表与统计口径一致。 */
  function showLibraryFilter(value: string) {
    selectLibrary('')
    setQuery('')
    setLibraryFilter(value)
  }

  /** 从异常分类进入知识库时直接定位对应任务；综合分类优先展示本库实际存在的异常。 */
  function openLibrary(id: string, counts: Record<string, number>) {
    let taskStatus = ''
    if (libraryFilter === 'attention') {
      taskStatus = attentionStatuses.find((value) => counts[value]) ?? ''
    } else if (attentionStatuses.some((value) => value === libraryFilter)) {
      taskStatus = libraryFilter
    }
    selectLibrary(id, taskStatus)
  }

  /** 显式重试只提交一次；成功后回读服务端状态，冲突与失败原样展示。 */
  async function retryJob(job: WikiJob) {
    if (!selectedId || retryLock.current) {
      return
    }
    retryLock.current = true
    setRetrying(true)
    setActionError('')
    setNotice('')
    setBatchResult(null)
    try {
      await wikiRequest(selectedId, `/jobs/${encodeURIComponent(job.id)}/retry`, 'POST')
      if (!mounted.current) {
        return
      }
      setNotice(`${job.id} 已提交重试，执行结果以最新任务状态为准。`)
      setRefreshVersion((version) => version + 1)
    } catch (reason) {
      if (mounted.current) {
        setActionError(reason instanceof Error ? reason.message : '重试失败')
      }
    } finally {
      retryLock.current = false
      if (mounted.current) {
        setRetrying(false)
      }
    }
  }

  /** 一次请求提交当前库所有分页的失败任务，与单条重试共享点击锁，随后回读真实状态。 */
  async function retryAllFailed() {
    if (!selectedId || retryLock.current) {
      return
    }
    retryLock.current = true
    setRetrying(true)
    setActionError('')
    setNotice('')
    setBatchResult(null)
    try {
      const result = await wikiRequest<WikiRetryBatchResult>(selectedId, '/retry-failed', 'POST')
      if (mounted.current) {
        setBatchResult(result)
      }
    } catch (reason) {
      if (mounted.current) {
        setActionError(`批量请求未确认完成：${reason instanceof Error ? reason.message : '请求失败'}。部分任务可能已排队，请先查看最新状态。`)
      }
    } finally {
      retryLock.current = false
      if (mounted.current) {
        setRetrying(false)
        setRefreshVersion((version) => version + 1)
      }
    }
  }

  let queueButtonLabel = data?.queue.paused ? '继续队列' : '停止队列'
  if (queueBusy) {
    queueButtonLabel = '保存中…'
  }

  return <section className="wiki-progress-page app-workspace" aria-labelledby="wiki-progress-title">
    <header className="workspace-page-header">
      <div><span className="workspace-page-kicker">知识维护工作区</span><h2 id="wiki-progress-title">整理总进度</h2><p>每个知识库的整理状态，都在这里。</p></div>
      <div className="wiki-progress-header-actions">
        <label><input type="checkbox" checked={autoRefresh} onChange={(event) => setAutoRefresh(event.target.checked)} />自动刷新</label>
        <button disabled={loading || retrying} onClick={() => setRefreshVersion((version) => version + 1)}><AppIcon name="refresh" size={16} />刷新</button>
        <button aria-label="返回聊天" title="返回聊天" onClick={onClose}><AppIcon name="chevronLeft" size={18} /></button>
      </div>
    </header>

    <div className="wiki-progress-scroll">
      <div className="wiki-progress-content">
        <div className="wiki-progress-intro"><div><span className="wiki-progress-eyebrow">WIKI / ACTIVITY</span><h1>让知识持续生长，<span>进展清晰可见。</span></h1></div><p>{refreshLabel}<br />{updatedAt && `最近更新 ${formatWikiTime(updatedAt)}`}</p></div>
        {error && <div role="alert" className="wiki-progress-error"><AppIcon name="alert" size={20} /><div><strong>无法读取最新进度</strong><p>{error}</p><p>{data ? '下方为上次成功读取的数据，不能视为当前状态。' : '暂时无法展示统计，请重新刷新。'}</p></div><button onClick={() => setRefreshVersion((version) => version + 1)}>重新加载</button></div>}

        <section className="wiki-progress-summary" aria-label="全部知识库整理统计" aria-busy={loading}>
          <div className="wiki-progress-completion"><span>任务完成率</span><div><strong>{data ? `${completion}%` : '—'}</strong><span>{data ? `${totals.succeeded} / ${totals.total} 个任务已完成` : '正在读取统计'}</span></div><progress aria-label="全部任务完成率" value={completion} max={100} /><small>仅已完成任务计入，部分完成与失败单独保留。</small></div>
          <div className="wiki-queue-summary">
            <span><i data-tone={data?.queue.paused ? 'warning' : 'active'} />{data?.queue.paused ? '队列已停止' : '正在整理'}</span>
            <strong>{data ? totals.running : '—'}{data?.queue.paused && <span> 个运行中</span>}</strong>
            <small>{totals.queued} 个任务排队中</small>
            <div className="wiki-queue-controls">
              <button disabled={!data || queueBusy || Boolean(error)} onClick={() => {
                if (data) {
                  void saveQueue({ paused: !data.queue.paused })
                }
              }}>
                {queueButtonLabel}
              </button>
              <form onSubmit={(event) => { event.preventDefault(); void saveQueue({ concurrency: Number(concurrencyDraft ?? data?.queue.concurrency) }) }}>
                <label htmlFor="wiki-queue-concurrency">同时整理</label>
                <div>
                  <input id="wiki-queue-concurrency" type="number" min={1} max={20} step={1} required disabled={!data || queueBusy || Boolean(error)} value={concurrencyDraft ?? data?.queue.concurrency ?? ''} onChange={(event) => setConcurrencyDraft(event.target.value)} aria-describedby="wiki-queue-explanation" />
                  <span>个</span>
                  <button type="submit" disabled={!data || queueBusy || Boolean(error) || !concurrencyDraft || Number(concurrencyDraft) === data.queue.concurrency}>保存</button>
                </div>
              </form>
            </div>
            <small id="wiki-queue-explanation">全站生效，1–20个；同库依次执行。停止后当前任务继续，排队任务保留。</small>
            {queueError && <small className="wiki-queue-error" role="alert">{queueError}</small>}
            {queueNotice && <small role="status">{queueNotice}</small>}
          </div>
          <div><span><i data-tone="warning" />异常任务</span><strong>{data ? totals.attention : '—'}</strong><small>按任务计数，资料另计</small>
            <div className="wiki-progress-summary-links" role="group" aria-label="异常任务分类">{attentionStatuses.map((value) => <button key={value} disabled={!data || loading || retrying || !totals[value]} onClick={() => showLibraryFilter(value)}>{wikiStatusLabel(value)} {data ? totals[value] : '—'}<AppIcon name="chevronRight" size={13} /></button>)}</div>
          </div>
          <div><span>待整理资料</span><strong>{data ? totals.pending : '—'}</strong><small>尚未完成整理的原始资料，含失败待重试</small><div className="wiki-progress-summary-links"><button disabled={!data || loading || retrying || !totals.pending} onClick={() => showLibraryFilter('pending')}>查看待整理知识库<AppIcon name="chevronRight" size={13} /></button></div></div>
        </section>
        {data && !data.autoCompileReady && <p className="wiki-progress-hint"><AppIcon name="info" size={17} />尚未配置第三方聊天模型，新资料的自动整理会跳过。请先在设置中配置模型。</p>}

        {actionError && <p role="alert" className="wiki-progress-error">操作未成功：{actionError}</p>}
        {notice && <p role="status" className="wiki-progress-hint" style={{ whiteSpace: 'pre-wrap' }}>{notice}</p>}
        {!selectedId ? <section aria-label="知识库进度列表">
          <div className="wiki-progress-section-heading"><div><h2>知识库进度</h2><p>点击知识库，查看任务明细与失败原因。</p></div><span>{libraries.length} 个知识库</span></div>
          <div className="wiki-progress-filters">
            <label className="wiki-progress-search"><AppIcon name="search" size={17} /><input type="search" aria-label="搜索知识库进度" placeholder="搜索知识库…" value={query} disabled={retrying} onChange={(event) => setQuery(event.target.value)} /></label>
            <div className="wiki-progress-filter-buttons" role="group" aria-label="知识库进度筛选">{[['all', '全部知识库'], ['active', '执行中'], ['attention', '异常任务'], ...attentionStatuses.map((value) => [value, wikiStatusLabel(value)]), ['pending', '待整理资料']].map(([value, label]) => <button key={value} aria-pressed={libraryFilter === value} disabled={retrying} onClick={() => setLibraryFilter(value)}>{label}</button>)}</div>
            {pendingControls}
          </div>
          <div className="wiki-library-list">
            {libraries.map((library) => {
              const counts = library.counts
              const attention = (counts.failed ?? 0) + (counts.partial ?? 0) + (counts.needs_split ?? 0)
              const done = counts.succeeded ?? 0
              const percent = library.total > 0 ? Math.floor(done / library.total * 100) : 0
              let idleLabel = '暂无执行中的任务'
              if (library.pendingCount > 0) {
                idleLabel = `${library.pendingCount} 份待整理`
              } else if (library.total === 0) {
                idleLabel = '尚未开始'
              } else if (done === library.total) {
                idleLabel = '全部完成'
              }
              return <button className="wiki-library-row" key={library.knowledgeBaseId} disabled={retrying} onClick={() => openLibrary(library.knowledgeBaseId, counts)} aria-label={`查看 ${library.name} 的整理进度`}>
                <span className="wiki-library-identity"><span className="wiki-library-icon"><AppIcon name="book" size={23} /></span><span><strong>{library.name}</strong><small>{library.pageCount} 个知识页 · {library.pendingCount} 份待整理资料{!library.enabled && ' · 自动整理已关闭'}</small></span></span>
                <span className="wiki-library-completion"><span><strong>{library.total ? `${percent}%` : '暂无任务'}</strong><small>{done} / {library.total} 已完成</small></span><span className="wiki-progress-track"><i style={{ width: `${percent}%` }} /></span></span>
                <span className="wiki-library-state">{Boolean(counts.running || counts.queued) && <span className="wiki-status-pill">{counts.running ?? 0} 整理中 · {counts.queued ?? 0} 排队</span>}{attentionStatuses.map((value) => Boolean(counts[value]) && <span key={value} className="wiki-status-pill" data-status={value}>{counts[value]} 个任务{wikiStatusLabel(value)}</span>)}{!attention && !counts.running && !counts.queued && <span className="wiki-library-idle">{idleLabel}</span>}</span>
                <AppIcon name="chevronRight" size={18} />
              </button>
            })}
          </div>
          {loading && <p role="status" className="wiki-progress-empty">正在读取各库进度…</p>}
          {data && !libraries.length && <div className="wiki-progress-empty"><AppIcon name="book" size={30} /><h3>{data.items.length ? '没有匹配的知识库' : '还没有知识库'}</h3><p>{data.items.length ? '调整搜索或筛选条件后再试。' : '创建知识库并上传资料后，就可以在这里追踪整理进度。'}</p></div>}
        </section> : <section aria-label="知识库任务详情">
          <div className="wiki-progress-detail-nav"><button onClick={() => selectLibrary('')} disabled={retrying}><AppIcon name="chevronLeft" size={16} />全部知识库</button><label>当前知识库<select aria-label="切换进度知识库" value={selectedId} disabled={loading || retrying} onChange={(event) => selectLibrary(event.target.value)}>{data?.items.map((library) => <option key={library.knowledgeBaseId} value={library.knowledgeBaseId}>{library.name}</option>)}</select></label></div>
          <div className="wiki-progress-section-heading"><div><h2>{selectedLibrary?.name ?? '知识库任务'}</h2><p>{selectedLibrary ? `${selectedLibrary.total} 个任务 · ${selectedLibrary.pageCount} 个知识页 · ${selectedLibrary.pendingCount} 份待整理资料` : '正在读取任务明细'}</p></div><span className="wiki-status-pill">{selectedLibrary?.enabled ? '自动整理开启' : '自动整理未开启'}</span></div>
          {selectedLibrary && !selectedLibrary.enabled && <p className="wiki-progress-hint"><AppIcon name="info" size={17} />本库自动整理已关闭。历史任务仍可查看，开启后可重试失败任务。</p>}
          <div className="wiki-progress-detail-actions">{pendingControls}</div>
          <section className="wiki-progress-task-toolbar" aria-label="任务筛选与操作">
            <div className="wiki-progress-task-toolbar-heading"><h3>整理任务</h3>
              <button disabled={retrying || loading || Boolean(error) || !selectedLibrary?.enabled || !(selectedLibrary.counts.failed || selectedLibrary.counts.needs_split)} onClick={() => void retryAllFailed()}><AppIcon name="refresh" size={16} />{retrying ? '正在提交重试…' : '重试本库失败任务'}</button>
            </div>
            <p>重试包含全部分页中的失败和暂停任务，保留已完成进度。</p>
            <div className="wiki-progress-task-filters" role="group" aria-label="任务状态筛选">{['', 'running', 'queued', 'failed', 'succeeded', 'partial', 'needs_split', 'canceled'].map((value) => <button key={value} aria-pressed={status === value} disabled={retrying} onClick={() => { setStatus(value); setOffset(0) }}>{value ? wikiStatusLabel(value) : '全部任务'}<span>{value ? selectedLibrary?.counts[value] ?? 0 : selectedLibrary?.total ?? 0}</span></button>)}</div>
          </section>
          {batchResult && <div className="wiki-progress-hint" role="status"><div>
            <strong>已提交 {batchResult.submitted} / {batchResult.total} 个失败任务</strong>
            <p>提交失败 {batchResult.failures.length} 个，尚未提交 {batchResult.remaining} 个。整理结果以任务最新状态为准。</p>
            {batchResult.failures.length > 0 && <details><summary>查看未提交成功的原因</summary><ul>{batchResult.failures.map((failure) => <li key={failure.jobId}>{failure.jobId}：{failure.error}</li>)}</ul></details>}
          </div></div>}
          {loading && <p role="status" className="wiki-progress-empty">正在读取任务…</p>}
          {data && <WikiJobList jobs={data.jobs.items} enabled={Boolean(selectedLibrary?.enabled)} busy={retrying || loading || Boolean(error)} onRetry={(job) => void retryJob(job)} />}
          {data && !data.jobs.items.length && <div className="wiki-progress-empty"><AppIcon name="clock" size={30} /><h3>{status ? '没有该状态的任务' : '还没有整理任务'}</h3><p>{status ? '可切换其他状态查看执行记录。' : '在知识文库中上传资料，或选择已有资料开始整理。'}</p></div>}
          {data && data.jobs.total > 0 && <nav className="wiki-progress-pagination" aria-label="任务分页"><span>共 {data.jobs.total} 个任务 · 第 {Math.floor(offset / 20) + 1} / {Math.ceil(data.jobs.total / 20)} 页</span><button disabled={offset === 0 || loading || retrying} onClick={() => setOffset(Math.max(0, offset - 20))}>上一页</button><button disabled={!data.jobs.nextCursor || loading || retrying} onClick={() => setOffset(Number(data.jobs.nextCursor))}>下一页</button></nav>}
        </section>}
      </div>
    </div>
  </section>

  /** 全库或当前库按资料排队；逐库回执保留部分失败，提交期间禁止重复操作。 */
  async function processPending() {
    if (retryLock.current || !data) {
      return
    }
    retryLock.current = true
    setRetrying(true)
    setNotice('')
    setActionError('')
    const results: string[] = []
    try {
      // 固定点击时的筛选结果；只提交按钮上显示的范围，不改变已关闭库的设置。
      for (const library of pendingLibraries) {
        try {
          const result = await wikiRequest<WikiBatchResult>(library.knowledgeBaseId, '/ingest-batch', 'POST', {})
          const failures = result.items.filter((item) => item.error)
          const queued = result.items.filter((item) => item.status === 'queued' || item.status === 'running').length
          results.push(`${library.name}：${queued} 份已排队或执行中，${failures.length} 份未提交${failures.length ? '；' + failures.map((item) => `${item.name}：${item.error}`).join('；') : ''}`)
        } catch (reason) {
          results.push(`${library.name}：请求未确认完成，${reason instanceof Error ? reason.message : '提交失败'}，请刷新核对`)
        }
        if (!mounted.current) {
          break
        }
      }
      if (mounted.current) {
        setNotice(results.join('\n') || '没有可提交的待整理资料；已关闭的知识库需先开启整理。')
      }
    } finally {
      retryLock.current = false
      if (mounted.current) {
        setRetrying(false)
        setRefreshVersion((version) => version + 1)
      }
    }
  }

  /** 保存后采用服务端回执；失败保留已生效值，离开页面不回写结果。 */
  async function saveQueue(settings: { paused?: boolean; concurrency?: number }) {
    if (queueLock.current || !data) {
      return
    }
    if (settings.concurrency !== undefined && (!Number.isInteger(settings.concurrency) || settings.concurrency < 1 || settings.concurrency > 20)) {
      setQueueError('同时整理数量需要在1到20之间')
      return
    }
    confirmedQueue.current = data.queue
    queueLock.current = true
    queueVersion.current += 1
    setQueueBusy(true)
    setQueueError('')
    setQueueNotice('')
    try {
      const queue = await updateWikiQueue(settings)
      if (!mounted.current) {
        return
      }
      confirmedQueue.current = queue
      setData((previous) => previous ? { ...previous, queue } : previous)
      if (settings.concurrency !== undefined) {
        setConcurrencyDraft(null)
      }
      setQueueNotice(queue.paused ? '已停止领取新任务，当前任务完成后停止。' : `队列运行中，最多同时整理 ${queue.concurrency} 个任务。`)
    } catch (reason) {
      if (mounted.current) {
        setQueueError(reason instanceof Error ? reason.message : '队列设置保存失败')
      }
    } finally {
      queueVersion.current += 1
      queueLock.current = false
      if (mounted.current) {
        setQueueBusy(false)
      }
    }
  }

}
