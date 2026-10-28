import { useEffect, useState } from 'react'
import type { KnowledgeBase } from '../App'
import { fetchWikiChanges, type WikiChange, type WikiList } from '../services/api'
import AppIcon from '../components/common/AppIcon'
import { formatWikiTime } from '../components/knowledge/WikiPanel'

/** 全库已发布文档历史；服务端统一分页，打开记录时保留库、页面和修订三个标识。 */
export default function WikiHistoryPage({ knowledgeBases, onOpenPage, onClose, onOpenProgress }: {
  knowledgeBases: KnowledgeBase[]
  onOpenPage: (change: WikiChange) => void
  onClose: () => void
  onOpenProgress: () => void
}) {
  const [knowledgeBaseId, setKnowledgeBaseId] = useState('')
  const [offset, setOffset] = useState(0)
  const [refreshVersion, setRefreshVersion] = useState(0)
  const [data, setData] = useState<WikiList<WikiChange> | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  // 筛选或离开页面时取消旧请求，失败不继续显示上一页为当前结果。
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setData(null)
    setError('')
    void fetchWikiChanges(knowledgeBaseId, offset, controller.signal).then((result) => {
      if (!controller.signal.aborted) {
        setData(result)
      }
    }).catch((reason: unknown) => {
      if (!controller.signal.aborted) {
        setError(reason instanceof Error ? reason.message : '读取整理历史失败')
      }
    }).finally(() => {
      if (!controller.signal.aborted) {
        setLoading(false)
      }
    })
    return () => controller.abort()
  }, [knowledgeBaseId, offset, refreshVersion])

  return <section className="wiki-history-page app-workspace" aria-label="全部整理历史">
    <header className="wiki-topbar">
      <div><h2>全部知识库的整理历史</h2><p>按发布时间查看整理成果，点击记录阅读当时的内容。</p></div>
      <div className="wiki-topbar-actions"><button onClick={onOpenProgress}>查看整理任务</button><button onClick={onClose}><AppIcon name="chevronLeft" size={16} />返回 Wiki</button></div>
    </header>
    <div className="wiki-history-content">
      <div className="wiki-history-filters">
        <label>知识库<select aria-label="筛选整理历史知识库" value={knowledgeBaseId} onChange={(event) => { setKnowledgeBaseId(event.target.value); setOffset(0) }}>
          <option value="">所有知识库</option>{knowledgeBases.map((kb) => <option key={kb.id} value={kb.id}>{kb.name}</option>)}
        </select></label>
        <span>{data ? `${data.total} 条发布记录` : '— 条发布记录'}</span>
        <button disabled={loading} onClick={() => setRefreshVersion((version) => version + 1)}><AppIcon name="refresh" size={16} />刷新历史</button>
      </div>
      <p className="wiki-history-note">这里记录已生成或更新的 Wiki 文档；失败、排队和无文档变更的任务可在“查看整理任务”中查看。</p>
      {error && <div role="alert" className="wiki-error"><p>{error}</p><button onClick={() => setRefreshVersion((version) => version + 1)}>重新加载历史</button></div>}
      {loading && <p role="status">正在读取全部整理历史…</p>}
      {data && <>
        <div className="wiki-history-columns" aria-hidden="true"><span>整理后的文档</span><span>所属知识库</span><span>发布时间</span><span /></div>
        <div className="wiki-history-list">{data.items.map((item) => <button className="wiki-history-row" key={`${item.knowledgeBaseId}-${item.pageId}-${item.revision}`} onClick={() => onOpenPage(item)}>
          <span><strong>{item.title}</strong><small>修订 {item.revision} · {item.sourceCount} 份来源</small></span>
          <span className="wiki-history-library">{item.knowledgeBaseName}</span>
          <time dateTime={item.createdAt}>{formatWikiTime(item.createdAt)}</time><AppIcon name="chevronRight" size={16} />
        </button>)}</div>
        {!data.items.length && <div className="wiki-simple-empty"><AppIcon name="clock" size={30} /><h3>暂无整理发布记录</h3><p>{offset ? '记录范围已变化，请返回第一页查看。' : '知识页生成或更新后，记录会显示在这里。'}</p>{offset > 0 && <button onClick={() => setOffset(0)}>返回第一页</button>}</div>}
        {(data.total > 0 || offset > 0) && <nav className="wiki-history-pagination" aria-label="全部整理历史分页"><span>第 {Math.floor(offset / 20) + 1} 页 · 共 {data.total} 条</span><button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 20))}>上一页</button><button disabled={!data.nextCursor} onClick={() => setOffset(Number(data.nextCursor))}>下一页</button></nav>}
      </>}
    </div>
  </section>
}
