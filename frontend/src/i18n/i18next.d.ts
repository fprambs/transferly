import 'i18next'

import type { defaultNS, resources } from './index'

declare module 'i18next' {
  interface CustomTypeOptions {
    enableSelector: true
    defaultNS: typeof defaultNS
    resources: (typeof resources)['en']
  }
}
