import AppIcon from '../common/AppIcon'
import type { AppIconName } from '../common/AppIcon'

type KnowledgeIconName =
  | 'book'
  | 'check'
  | 'chevronDown'
  | 'chevronLeft'
  | 'chevronUp'
  | 'database'
  | 'file'
  | 'folderPlus'
  | 'plus'
  | 'search'
  | 'trash'
  | 'upload'
  | 'x'

interface KnowledgeIconProps {
  name: KnowledgeIconName
  size?: number
}

// 知识库区域统一走公共图标映射，避免不同操作继续混用 Emoji 和文本符号。
const KnowledgeIcon = ({ name, size }: KnowledgeIconProps) => {
  const iconMap: Record<KnowledgeIconName, AppIconName> = {
    book: 'book',
    check: 'check',
    chevronDown: 'chevronDown',
    chevronLeft: 'chevronLeft',
    chevronUp: 'chevronUp',
    database: 'database',
    file: 'file',
    folderPlus: 'folderPlus',
    plus: 'plus',
    search: 'search',
    trash: 'trash',
    upload: 'upload',
    x: 'x',
  }

  return <AppIcon name={iconMap[name]} size={size} />
}

export default KnowledgeIcon
