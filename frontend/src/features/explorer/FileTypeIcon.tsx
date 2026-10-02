import {
  File,
  FileArchive,
  FileBadge,
  FileBox,
  FileBraces,
  FileChartPie,
  FileClock,
  FileCode,
  FileCog,
  FileHeadphone,
  FileImage,
  FileKey,
  FilePlay,
  FileQuestion,
  FileSliders,
  FileSpreadsheet,
  FileStack,
  FileTerminal,
  FileText,
  FileType as FileFont,
  Folder,
  Link2,
  type LucideIcon,
} from 'lucide-react'

import { cn } from '@/lib/utils'

import type { FileType } from './file-types'

/** Icon and color of each type; kinds left out get a plain file. */
const icons: Partial<Record<FileType['kind'], readonly [LucideIcon, string]>> = {
  directory: [Folder, 'fill-accent-text/20 text-accent-text'],
  symlink: [Link2, 'text-info'],
  other: [FileQuestion, 'text-muted-foreground'],
  image: [FileImage, 'text-ok'],
  audio: [FileHeadphone, 'text-info'],
  video: [FilePlay, 'text-info'],
  archive: [FileArchive, 'text-warn'],
  package: [FileBox, 'text-warn'],
  disk: [FileStack, 'text-warn'],
  font: [FileFont, 'text-muted-foreground'],
  code: [FileCode, 'text-accent-text'],
  script: [FileTerminal, 'text-accent-text'],
  document: [FileText, 'text-info'],
  spreadsheet: [FileSpreadsheet, 'text-ok'],
  presentation: [FileChartPie, 'text-warn'],
  data: [FileBraces, 'text-info'],
  config: [FileSliders, 'text-muted-foreground'],
  named: [FileCog, 'text-muted-foreground'],
  text: [FileText, 'text-muted-foreground'],
  log: [FileClock, 'text-muted-foreground'],
  backup: [FileClock, 'text-warn'],
  key: [FileKey, 'text-destructive'],
  certificate: [FileBadge, 'text-destructive'],
  executable: [FileCog, 'text-warn'],
}

/** Icon that tells the type of an entry at a glance (4.14e): a shape for each family of formats and a soft color for each group, folders filled. */
export function FileTypeIcon({ type, className }: { type: FileType; className?: string }) {
  const [Icon, color] = icons[type.kind] ?? [File, 'text-muted-foreground']
  return <Icon aria-hidden="true" className={cn('size-4 shrink-0', color, className)} />
}
