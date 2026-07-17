import type { ReactNode } from 'react'

interface EmptyStateProps {
  icon?: ReactNode
  title: string
  description?: string
  actionLabel?: string
  onAction?: () => void
  className?: string
}

// 统一空状态的信息层级，业务组件只负责提供文案和可选操作。
const EmptyState = ({
  icon,
  title,
  description,
  actionLabel,
  onAction,
  className = '',
}: EmptyStateProps) => (
  <section className={`empty-state ${className}`} aria-label={title}>
    {icon && <div className="empty-state-icon">{icon}</div>}
    <div className="empty-state-content">
      <h4 className="empty-state-title">{title}</h4>
      {description && <p className="empty-state-description">{description}</p>}
    </div>
    {actionLabel && onAction && (
      <button type="button" className="btn-primary" onClick={onAction}>
        {actionLabel}
      </button>
    )}
  </section>
)

export default EmptyState
