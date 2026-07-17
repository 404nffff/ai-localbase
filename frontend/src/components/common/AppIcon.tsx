import type { FC } from 'react'
import type { LucideIcon } from 'lucide-react'
import {
  BookOpen,
  Brain,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  Clock3,
  Copy,
  Database,
  Download,
  Eye,
  EyeOff,
  FileText,
  FolderPlus,
  Info,
  KeyRound,
  LoaderCircle,
  MessageSquare,
  Moon,
  MoreHorizontal,
  PanelLeftClose,
  PanelLeftOpen,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Send,
  Settings,
  Sparkles,
  Sun,
  Trash2,
  TriangleAlert,
  Upload,
  UserRound,
  X,
  Zap,
} from 'lucide-react'

export type AppIconName =
  | 'alert'
  | 'book'
  | 'brain'
  | 'check'
  | 'chevronDown'
  | 'chevronLeft'
  | 'chevronRight'
  | 'chevronUp'
  | 'clock'
  | 'copy'
  | 'database'
  | 'download'
  | 'eye'
  | 'eyeOff'
  | 'file'
  | 'folderPlus'
  | 'info'
  | 'key'
  | 'loader'
  | 'message'
  | 'moon'
  | 'more'
  | 'panelClose'
  | 'panelOpen'
  | 'pencil'
  | 'plus'
  | 'refresh'
  | 'search'
  | 'send'
  | 'settings'
  | 'sparkles'
  | 'sun'
  | 'trash'
  | 'upload'
  | 'user'
  | 'x'
  | 'zap'

// 集中维护页面图标映射，避免各业务组件重复导入图标库并产生不一致的描边参数。
const icons: Record<AppIconName, LucideIcon> = {
  alert: TriangleAlert,
  book: BookOpen,
  brain: Brain,
  check: Check,
  chevronDown: ChevronDown,
  chevronLeft: ChevronLeft,
  chevronRight: ChevronRight,
  chevronUp: ChevronUp,
  clock: Clock3,
  copy: Copy,
  database: Database,
  download: Download,
  eye: Eye,
  eyeOff: EyeOff,
  file: FileText,
  folderPlus: FolderPlus,
  info: Info,
  key: KeyRound,
  loader: LoaderCircle,
  message: MessageSquare,
  moon: Moon,
  more: MoreHorizontal,
  panelClose: PanelLeftClose,
  panelOpen: PanelLeftOpen,
  pencil: Pencil,
  plus: Plus,
  refresh: RefreshCw,
  search: Search,
  send: Send,
  settings: Settings,
  sparkles: Sparkles,
  sun: Sun,
  trash: Trash2,
  upload: Upload,
  user: UserRound,
  x: X,
  zap: Zap,
}

interface AppIconProps {
  className?: string
  name: AppIconName
  size?: number
  strokeWidth?: number
}

const AppIcon: FC<AppIconProps> = ({
  className,
  name,
  size = 18,
  strokeWidth = 1.75,
}) => {
  const Icon = icons[name]
  return <Icon aria-hidden="true" className={className} size={size} strokeWidth={strokeWidth} />
}

export default AppIcon
