import React, { useState } from 'react'

interface UploadDropZoneProps {
  onFilesSelected: (files: FileList) => void
  children?: React.ReactNode
}

const UploadDropZone: React.FC<UploadDropZoneProps> = ({ onFilesSelected, children }) => {
  const [isDragging, setIsDragging] = useState(false)

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setIsDragging(false)

    const files = e.dataTransfer.files
    if (files.length > 0) {
      onFilesSelected(files)
    }
  }

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setIsDragging(true)
  }

  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    setIsDragging(false)
  }

  return (
    <div
      className={`upload-drop-zone ${isDragging ? 'dragging' : ''}`}
      onDrop={handleDrop}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
    >
      {children}
      {isDragging && (
        <div className="upload-drop-overlay">
          <div className="upload-drop-hint">释放以上传文件</div>
        </div>
      )}
    </div>
  )
}

export default UploadDropZone
