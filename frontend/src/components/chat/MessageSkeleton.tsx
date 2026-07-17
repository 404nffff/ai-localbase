const MessageSkeleton = () => (
  <div className="message assistant loading" role="status" aria-live="polite">
    <div className="message-content message-content-thinking">
      <div className="thinking-indicator" aria-label="AI 正在生成回答">
        <span className="thinking-dot" />
        <span className="thinking-dot" />
        <span className="thinking-dot" />
      </div>
    </div>
  </div>
)

export default MessageSkeleton
