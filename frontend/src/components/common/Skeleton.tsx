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

export default Skeleton
