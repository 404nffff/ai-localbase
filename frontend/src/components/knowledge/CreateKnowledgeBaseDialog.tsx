import React from 'react'

interface CreateKnowledgeBaseDialogProps {
  name: string
  description: string
  onNameChange: (value: string) => void
  onDescriptionChange: (value: string) => void
  onCancel: () => void
  onConfirm: () => void
}

const CreateKnowledgeBaseDialog: React.FC<CreateKnowledgeBaseDialogProps> = ({
  name,
  description,
  onNameChange,
  onDescriptionChange,
  onCancel,
  onConfirm,
}) => (
  <div className="kb-create-backdrop" onClick={onCancel}>
    <div className="kb-create-dialog" onClick={(event) => event.stopPropagation()}>
      <div className="kb-create-dialog-header">
        <h3>新建知识库</h3>
        <button className="kb-close-btn" onClick={onCancel}>x</button>
      </div>
      <div className="kb-create-dialog-body">
        <div className="kb-form-field">
          <label className="kb-form-label">知识库名称 <span className="kb-required">*</span></label>
          <input
            className="kb-form-input"
            type="text"
            placeholder="例如：产品文档、技术手册"
            value={name}
            onChange={(event) => onNameChange(event.target.value)}
            onKeyDown={(event) => event.key === 'Enter' && onConfirm()}
            autoFocus
            maxLength={50}
          />
        </div>
        <div className="kb-form-field">
          <label className="kb-form-label">描述（可选）</label>
          <textarea
            className="kb-form-textarea"
            placeholder="简要描述该知识库的用途"
            value={description}
            onChange={(event) => onDescriptionChange(event.target.value)}
            rows={3}
            maxLength={200}
          />
        </div>
      </div>
      <div className="kb-create-dialog-footer">
        <button className="kb-cancel-btn" onClick={onCancel}>取消</button>
        <button className="kb-confirm-btn" onClick={onConfirm} disabled={!name.trim()}>
          创建知识库
        </button>
      </div>
    </div>
  </div>
)

export default CreateKnowledgeBaseDialog
