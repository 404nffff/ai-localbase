import React from 'react'

const Skeleton: React.FC<{ count?: number }> = ({ count = 5 }) => {
  return (
    <div className="skeleton-list">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="skeleton-item">
          <div className="skeleton-line skeleton-title" />
          <div className="skeleton-line skeleton-text" />
          <div className="skeleton-line skeleton-text short" />
        </div>
      ))}
    </div>
  )
}

export interface SkeletonBlockProps {
  width?: string
  height?: string
  className?: string
  lines?: number
}

export const SkeletonBlock: React.FC<SkeletonBlockProps> = ({
  width = '100%',
  height = '16px',
  className = '',
  lines = 1,
}) => {
  if (lines > 1) {
    return (
      <div className={`skeleton-block ${className}`}>
        {Array.from({ length: lines }).map((_, i) => (
          <div
            key={i}
            className="skeleton-line"
            style={{ width: `${100 - (i * 15)}%`, height, marginBottom: i < lines - 1 ? '8px' : '0' }}
          />
        ))}
      </div>
    )
  }

  return (
    <div
      className={`skeleton-block ${className}`}
      style={{ width, height }}
    />
  )
}

export default Skeleton
