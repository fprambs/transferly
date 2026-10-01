import { Lock } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Page, PageHeader } from '@/components/page/Page'

import { SecuritySection } from './SecuritySection'

/** The security section of the settings: how the account signs in, with its second step and passkeys. */
export function SecurityPage() {
  const { t } = useTranslation('settings')

  return (
    <Page>
      <PageHeader icon={Lock} title={t(($) => $.securityPage.title)} description={t(($) => $.securityPage.description)} />
      <SecuritySection />
    </Page>
  )
}
