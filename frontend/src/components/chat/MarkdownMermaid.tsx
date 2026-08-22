import React, { useEffect, useState } from 'react'

interface MermaidDiagramProps {
  chart: string
}

const MarkdownMermaid: React.FC<MermaidDiagramProps> = ({ chart }) => {
  const [svg, setSvg] = useState('')
  const [error, setError] = useState('')
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    let timedOut = false
    let timeout: number | null = null
    setSvg('')
    setError('')
    setIsLoading(true)

    const clearRenderTimeout = () => {
      if (timeout !== null) {
        window.clearTimeout(timeout)
        timeout = null
      }
    }

    const renderChart = async () => {
      try {
        const { default: mermaid } = await import('mermaid')
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: 'strict',
          theme: 'default',
        })
        const id = `mermaid-${Math.random().toString(36).slice(2, 10)}`
        const { svg: renderedSvg } = await mermaid.render(id, chart)

        const hasSvgContent = Boolean(renderedSvg && renderedSvg.includes('<svg'))
        const hasSyntaxError = /Syntax error in text|Parse error|Lexical error/i.test(renderedSvg)
        const hasUnsafeSvg = /<script|on[a-z]+\s*=|javascript:/i.test(renderedSvg)
        if (!hasSvgContent || hasSyntaxError || hasUnsafeSvg) {
          throw new Error('invalid mermaid svg')
        }

        if (!cancelled && !timedOut) {
          clearRenderTimeout()
          setSvg(renderedSvg)
          setError('')
          setIsLoading(false)
        }
      } catch {
        if (!cancelled && !timedOut) {
          clearRenderTimeout()
          setSvg('')
          setError('流程图渲染失败，已降级显示源码')
          setIsLoading(false)
        }
      }
    }

    void renderChart()

    timeout = window.setTimeout(() => {
      timeout = null
      timedOut = true
      if (!cancelled) {
        setSvg('')
        setError('流程图渲染超时，已降级显示源码')
        setIsLoading(false)
      }
    }, 2500)

    return () => {
      cancelled = true
      clearRenderTimeout()
    }
  }, [chart])

  if (error) {
    return (
      <div className="md-mermaid-fallback">
        <div className="md-mermaid-error">{error}</div>
        <pre className="md-code-block">
          <code>{chart}</code>
        </pre>
      </div>
    )
  }

  if (isLoading) {
    return <div className="md-mermaid-loading">流程图渲染中...</div>
  }

  if (!svg) {
    return (
      <div className="md-mermaid-fallback">
        <div className="md-mermaid-error">流程图无有效输出，已降级显示源码</div>
        <pre className="md-code-block">
          <code>{chart}</code>
        </pre>
      </div>
    )
  }

  return <div className="md-mermaid" dangerouslySetInnerHTML={{ __html: svg }} />
}

export default MarkdownMermaid
