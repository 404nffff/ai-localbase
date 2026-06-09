import React, { useState } from 'react'
import { testChatModelConfig, testEmbeddingModelConfig } from '../../services/api'
import type { ChatConfig, EmbeddingConfig } from '../../App'
import type { TestModelResponse } from '../../services/api'

type ModelConfigTestProps =
  | {
      type: 'chat'
      provider: ChatConfig['provider']
      baseUrl: string
      modelName: string
      apiKey: string
      temperature: number
    }
  | {
      type: 'embedding'
      provider: EmbeddingConfig['provider']
      baseUrl: string
      modelName: string
      apiKey: string
      temperature?: never
    }

const testLabelByType: Record<ModelConfigTestProps['type'], string> = {
  chat: '测试聊天模型',
  embedding: '测试 Embedding',
}

const defaultSuccessMessage: Record<ModelConfigTestProps['type'], string> = {
  chat: '聊天模型连接正常',
  embedding: 'Embedding 模型连接正常',
}

export const ModelConfigTest: React.FC<ModelConfigTestProps> = (props) => {
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<TestModelResponse | null>(null)
  const [errorMessage, setErrorMessage] = useState('')

  const hasRequiredConfig = Boolean(props.baseUrl.trim() && props.modelName.trim())
  const buttonLabel = testing ? '测试中...' : testLabelByType[props.type]

  const handleTest = async () => {
    if (!hasRequiredConfig || testing) {
      return
    }

    setTesting(true)
    setResult(null)
    setErrorMessage('')

    try {
      const nextResult =
        props.type === 'chat'
          ? await testChatModelConfig({
              provider: props.provider,
              baseUrl: props.baseUrl,
              model: props.modelName,
              apiKey: props.apiKey,
              temperature: props.temperature,
              contextMessageLimit: 1,
            })
          : await testEmbeddingModelConfig({
              provider: props.provider,
              baseUrl: props.baseUrl,
              model: props.modelName,
              apiKey: props.apiKey,
            })

      setResult(nextResult)
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : '模型测试请求失败')
    } finally {
      setTesting(false)
    }
  }

  const message =
    result?.model_info ||
    result?.error_message ||
    errorMessage ||
    (hasRequiredConfig ? '' : '请先填写 Base URL 和 Model')

  return (
    <div className="model-config-test">
      <button
        type="button"
        className="test-connection-btn"
        onClick={() => void handleTest()}
        disabled={!hasRequiredConfig || testing}
      >
        {buttonLabel}
      </button>

      {message ? (
        <div
          className={`test-result ${
            result?.success ? 'test-success' : 'test-error'
          }`}
          role="status"
          aria-live="polite"
        >
          <span className="test-icon">{result?.success ? 'OK' : '!'}</span>
          <div className="test-details">
            <span className="test-message">
              {result?.success ? defaultSuccessMessage[props.type] : message}
            </span>
            {result?.success && result.model_info ? (
              <span className="test-info">{result.model_info}</span>
            ) : null}
            {typeof result?.latency_ms === 'number' ? (
              <span className="test-latency">{result.latency_ms} ms</span>
            ) : null}
            {typeof result?.vector_size === 'number' ? (
              <span className="test-info">Vector size: {result.vector_size}</span>
            ) : null}
            {!result?.success && result?.error_message ? (
              <span className="test-error-msg">{result.error_message}</span>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  )
}
