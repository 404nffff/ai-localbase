import { useEffect, useRef, useState, type FormEvent } from 'react'
import { fetchWikiPageForEdit, wikiRequest, type WikiPageContent, type WikiPageEditResult } from '../../services/api'
import MarkdownRenderer from '../chat/LazyMarkdownRenderer'
import AppIcon from '../common/AppIcon'

/**
 * 在模态窗口中编辑完整当前修订；保存期间禁止重复提交，失败保留草稿。
 * 历史入口也读取当前版本，关闭或刷新前提示未保存修改，避免覆盖旧历史。
 */
export default function WikiPageEditor({ knowledgeBaseId, pageId, onClose, onSaved }: {
  /** 编辑范围始终由已打开的知识页决定。 */
  knowledgeBaseId: string
  pageId: string
  onClose: () => void
  /** 只有正文发布成功才通知宿主刷新；索引失败仍保留成功回执。 */
  onSaved: (result: WikiPageEditResult) => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const titleInput = useRef<HTMLInputElement>(null)
  const savingRef = useRef(false)
  const [baseline, setBaseline] = useState<WikiPageContent | null>(null)
  const [title, setTitle] = useState('')
  const [markdown, setMarkdown] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [preview, setPreview] = useState(false)
  const [error, setError] = useState('')
  const [readVersion, setReadVersion] = useState(0)
  const dirty = baseline !== null && (title !== baseline.title || markdown !== baseline.markdown)

  useEffect(() => {
    const element = dialog.current
    element?.showModal()
    return () => element?.close()
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    void fetchWikiPageForEdit(knowledgeBaseId, pageId, controller.signal).then((page) => {
      if (!controller.signal.aborted) {
        setBaseline(page)
        setTitle(page.title)
        setMarkdown(page.markdown)
      }
    }).catch((reason: unknown) => {
      if (!controller.signal.aborted) {
        setError(reason instanceof Error ? reason.message : '读取知识页失败')
      }
    }).finally(() => {
      if (!controller.signal.aborted) {
        setLoading(false)
      }
    })
    return () => controller.abort()
  }, [knowledgeBaseId, pageId, readVersion])

  useEffect(() => {
    if (!loading) {
      titleInput.current?.focus()
    }
  }, [loading])

  useEffect(() => {
    /** 浏览器刷新或关闭时提示尚未保存的草稿。 */
    function preventUnload(event: BeforeUnloadEvent) {
      if (dirty || saving) {
        event.preventDefault()
        event.returnValue = ''
      }
    }
    window.addEventListener('beforeunload', preventUnload)
    return () => window.removeEventListener('beforeunload', preventUnload)
  }, [dirty, saving])

  return <dialog ref={dialog} className="wiki-editor-dialog" aria-labelledby="wiki-editor-title" onCancel={(event) => {
    event.preventDefault()
    requestClose()
  }}>
    <form onSubmit={(event) => void save(event)}>
      <header className="wiki-editor-heading"><div><h2 id="wiki-editor-title">编辑知识页</h2><p>{baseline ? `基于当前修订 ${baseline.revision}，保存后生成新修订。` : '正在读取完整当前版本…'}</p></div><button type="button" aria-label="关闭编辑" disabled={saving} onClick={requestClose}><AppIcon name="x" size={20} /></button></header>
      {error && <div role="alert" className="wiki-editor-error"><p>{error}</p>{baseline && <p>草稿仍保留在编辑框中，请核对后再操作。</p>}<button type="button" disabled={saving || loading} onClick={reload}>重新读取当前版本</button></div>}
      {loading && <p role="status" className="wiki-editor-loading">正在读取完整正文…</p>}
      {baseline && !loading && <div className="wiki-editor-fields">
        <label>知识页标题<input ref={titleInput} aria-label="知识页标题" value={title} disabled={saving} required onChange={(event) => setTitle(event.target.value)} /></label>
        <div className="wiki-editor-tabs" role="group" aria-label="正文编辑模式"><button type="button" aria-pressed={!preview} onClick={() => setPreview(false)}>编辑正文</button><button type="button" aria-pressed={preview} onClick={() => setPreview(true)}>预览效果</button><span>支持 Markdown</span></div>
        {preview ? <section className="wiki-editor-preview" aria-label="正文预览"><MarkdownRenderer content={markdown} preserveSource /></section> : <label className="wiki-editor-body-label"><span className="sr-only">知识页正文</span><textarea aria-label="知识页正文" value={markdown} disabled={saving} required spellCheck={false} onChange={(event) => setMarkdown(event.target.value)} /></label>}
      </div>}
      <footer className="wiki-editor-footer"><span>{dirty ? '有未保存的修改' : '原始资料和历史修订保留'}</span><button type="button" disabled={saving} onClick={requestClose}>取消</button><button className="wiki-primary" disabled={loading || saving || !baseline || !dirty || !title.trim() || !markdown.trim()} type="submit">{saving ? '正在保存…' : '保存修改'}</button></footer>
    </form>
  </dialog>

  /** 只有用户放弃草稿时才退出；保存中不取消已经发出的发布请求。 */
  function requestClose() {
    if (savingRef.current || (dirty && !window.confirm('修改尚未保存，确定放弃本次编辑吗？'))) {
      return
    }
    onClose()
  }

  /** 冲突后由用户明确选择重新读取，避免悄悄用新基线覆盖当前草稿。 */
  function reload() {
    if (dirty && !window.confirm('重新读取会替换当前草稿，请先复制需要保留的修改。是否继续？')) {
      return
    }
    setReadVersion((version) => version + 1)
  }

  /** 保存完整草稿与基线；发布错误保持表单，索引错误交由宿主展示已保存状态。 */
  async function save(event: FormEvent) {
    event.preventDefault()
    if (savingRef.current || loading || !baseline || !dirty || !title.trim() || !markdown.trim()) {
      return
    }
    savingRef.current = true
    setSaving(true)
    setError('')
    try {
      const result = await wikiRequest<WikiPageEditResult>(knowledgeBaseId, `/pages/${encodeURIComponent(pageId)}`, 'PUT', { baseRevision: baseline.revision, title, markdown })
      onSaved(result)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '保存结果未确认，请保留草稿并核对当前版本')
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }
}
