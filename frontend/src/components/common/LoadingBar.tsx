import React from 'react'

const LoadingBar: React.FC<{ loading: boolean }> = ({ loading }) => {
  if (!loading) return null

  return (
    <div className="loading-bar">
      <div className="loading-bar-progress" />
    </div>
  )
}

export default LoadingBar
