import React from 'react'

interface EmptyStateProps {
  icon?: React.ReactNode
  title: string
  description?: string
  actionLabel?: string
  onAction?: () => void
  className?: string
}

const EmptyState: React.FC<EmptyStateProps> = ({
  icon,
  title,
  description,
  actionLabel,
  onAction,
  className = '',
}) => {
  const handleAction = () => {
    onAction?.()
  }

  return (
    <div className={`empty-state ${className}`}>
      <div className="empty-state-icon">
        {icon || (
          <svg
            width="48"
            height="48"
            viewBox="0 0 48 48"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <circle
              cx="24"
              cy="24"
              r="20"
              fill="var(--bg-tertiary)"
            />
            <path
              d="M16 20C16 18.8954 16.8954 18 18 18H30C31.1046 18 32 18.8954 32 20V28C32 29.1046 31.1046 30 30 30H18C16.8954 30 16 29.1046 16 28V20Z"
              stroke="var(--text-tertiary)"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            <path
              d="M20 24H28"
              stroke="var(--text-tertiary)"
              strokeWidth="2"
              strokeLinecap="round"
            />
          </svg>
        )}
      </div>
      <div className="empty-state-content">
        <h4 className="empty-state-title">{title}</h4>
        {description && <p className="empty-state-description">{description}</p>}
      </div>
      {actionLabel && onAction && (
        <button
          type="button"
          className="btn-primary"
          onClick={handleAction}
        >
          {actionLabel}
        </button>
      )}
    </div>
  )
}

export default EmptyState
