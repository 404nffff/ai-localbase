import React, { useState } from 'react'

interface UploadDropZoneProps {
  onFilesSelected: (files: FileList) => void
  children?: React.ReactNode
}

const UploadDropZone: React.FC<UploadDropZoneProps> = ({ onFilesSelected, children }) => {
  const [isDragging, setIsDragging] = useState(false)

  return (
    <div
      className={`upload-drop-zone ${isDragging ? 'dragging' : ''}`}
      onDrop={(e) => { 
        e.preventDefault(); 
        setIsDragging(false); 
        if (e.dataTransfer.files.length > 0) onFilesSelected(e.dataTransfer.files)
      }}
      onDragOver={(e) => { e.preventDefault(); setIsDragging(true) }}
      onDragLeave={() => setIsDragging(false)}
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
