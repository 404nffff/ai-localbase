import { Component, lazy, Suspense, type ReactNode } from 'react'

const Renderer = lazy(() => import('./MarkdownRenderer'))

/** 加载失败仍展示原文，错误明确可见，不把空白当作渲染成功。 */
class RenderBoundary extends Component<{ children: ReactNode; content: string }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  render() {
    if (this.state.failed) return <div role="alert"><p>格式化组件加载失败，请刷新重试。</p><pre>{this.props.content}</pre></div>
    return this.props.children
  }
}

/** 有正文时才加载 Markdown 解析器，Wiki 原文模式原样传递。 */
export default function LazyMarkdownRenderer(props: { content: string; preserveSource?: boolean }) {
  if (!props.content) return null
  return <RenderBoundary content={props.content}>
    <Suspense fallback={<pre aria-busy="true">{props.content}</pre>}><Renderer {...props} /></Suspense>
  </RenderBoundary>
}
