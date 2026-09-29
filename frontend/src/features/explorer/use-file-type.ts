import { useTranslation } from 'react-i18next'

import type { RemoteEntry } from '@/lib/api'

import { fileTypeOf } from './file-types'

/** Returns what describes the type of an entry in words, such as "PNG image" or "Folder" (4.14e). */
export function useDescribeType(): (entry: Pick<RemoteEntry, 'name' | 'type'>) => string {
  const { t } = useTranslation('explorer')
  return (entry) => {
    const type = fileTypeOf(entry)
    return t(($) => $.fileTypes[type.kind], { name: type.name })
  }
}
