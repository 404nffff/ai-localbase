import React from 'react'

interface UploadPreviewProps {
  files: File[]
  onRemoveFile: (index: number) => void
}

const formatFileSize = (bytes: number): string => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
}

const getFileIcon = (type: string): string => {
  if (type.startsWith('image/')) return '🖼️'
  if (type.startsWith('video/')) return '🎬'
  if (type.startsWith('audio/')) return '🎵'
  if (type.includes('pdf')) return '📄'
  if (type.includes('word') || type.includes('document')) return '📝'
  if (type.includes('sheet') || type.includes('excel')) return '📊'
  if (type.includes('presentation') || type.includes('powerpoint')) return '📽️'
  if (type.includes('zip') || type.includes('rar') || type.includes('compressed')) return '🗜️'
  if (type.includes('text')) return '📃'
  return '📁'
}

const UploadPreview: React.FC<UploadPreviewProps> = ({ files, onRemoveFile }) => {
  const totalSize = files.reduce((sum, file) => sum + file.size, 0)

  return (
    <div className="upload-preview">
      <div className="upload-preview-header">
        <span className="upload-preview-count">{files.length} 个文件</span>
        <span className="upload-preview-size">总大小: {formatFileSize(totalSize)}</span>
      </div>
      <div className="upload-preview-list">
        {files.map((file, index) => (
          <div key={`${file.name}-${index}`} className="upload-preview-item">
            <span className="upload-preview-icon">{getFileIcon(file.type)}</span>
            <div className="upload-preview-info">
              <span className="upload-preview-name">{file.name}</span>
              <span className="upload-preview-meta">
                {formatFileSize(file.size)} · {file.type || '未知类型'}
              </span>
            </div>
            <button
              className="upload-preview-remove"
              onClick={() => onRemoveFile(index)}
              title="移除文件"
            >
              ×
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}

export default UploadPreview
