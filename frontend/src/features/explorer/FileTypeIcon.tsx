import {
  File,
  FileArchive,
  FileBox,
  FileCode,
  FileCog,
  FileImage,
  FileKey,
  FileMusic,
  FileQuestion,
  FileSpreadsheet,
  FileTerminal,
  FileText,
  FileType as FileFont,
  FileVideoCamera,
  Folder,
  Link2,
  type LucideIcon,
} from 'lucide-react'

import { cn } from '@/lib/utils'

import type { FileType } from './file-types'

/** Icon and color of each type; kinds left out get a plain file. */
const icons: Partial<Record<FileType['kind'], readonly [LucideIcon, string]>> = {
  directory: [Folder, 'text-accent-text'],
  symlink: [Link2, 'text-info'],
  other: [FileQuestion, 'text-muted-foreground'],
  image: [FileImage, 'text-ok'],
  audio: [FileMusic, 'text-info'],
  video: [FileVideoCamera, 'text-info'],
  archive: [FileArchive, 'text-warn'],
  package: [FileBox, 'text-warn'],
  disk: [FileBox, 'text-warn'],
  font: [FileFont, 'text-muted-foreground'],
  code: [FileCode, 'text-accent-text'],
  script: [FileTerminal, 'text-accent-text'],
  document: [FileText, 'text-info'],
  spreadsheet: [FileSpreadsheet, 'text-ok'],
  presentation: [FileText, 'text-warn'],
  data: [FileCode, 'text-muted-foreground'],
  config: [FileCog, 'text-muted-foreground'],
  named: [FileCog, 'text-muted-foreground'],
  text: [FileText, 'text-muted-foreground'],
  log: [FileText, 'text-muted-foreground'],
  key: [FileKey, 'text-destructive'],
  certificate: [FileKey, 'text-destructive'],
  executable: [FileCog, 'text-warn'],
}

/** Icon that tells the type of an entry at a glance (4.14e). */
export function FileTypeIcon({ type, className }: { type: FileType; className?: string }) {
  const [Icon, color] = icons[type.kind] ?? [File, 'text-muted-foreground']
  return <Icon aria-hidden="true" className={cn('size-4 shrink-0', color, className)} />
}
