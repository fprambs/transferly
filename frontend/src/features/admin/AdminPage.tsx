import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { useSession } from '../auth/use-session'

import { AccountsTab } from './AccountsTab'
import { AdminAuditTab } from './AdminAuditTab'
import { LimitsTab } from './LimitsTab'
import { NetworksTab } from './NetworksTab'

/** The tabs of the page. */
type PageTab = 'accounts' | 'networks' | 'limits' | 'audit'

/** The administration page of the instance: accounts, private networks and the record of what administrators did. Only administrators see it. */
export function AdminPage() {
  const { t } = useTranslation('admin')
  const session = useSession()
  const [tab, setTab] = useState<PageTab>('accounts')

  if (session === null) {
    return <p className="p-7 text-sm text-muted-foreground">{t(($) => $.page.loading)}</p>
  }
  if (session.user.role !== 'admin') {
    return <p className="p-7 text-sm text-muted-foreground">{t(($) => $.page.noAccess)}</p>
  }
  return (
    <div className="flex flex-col gap-4 p-7">
      <header>
        <h1 className="text-xl font-semibold">{t(($) => $.page.title)}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t(($) => $.page.subtitle)}</p>
      </header>
      <Tabs value={tab} onValueChange={(value) => setTab(value as PageTab)}>
        <TabsList variant="line" className="justify-start">
          <TabsTrigger value="accounts" className="flex-none px-3">
            {t(($) => $.page.tabs.accounts)}
          </TabsTrigger>
          <TabsTrigger value="networks" className="flex-none px-3">
            {t(($) => $.page.tabs.networks)}
          </TabsTrigger>
          <TabsTrigger value="limits" className="flex-none px-3">
            {t(($) => $.page.tabs.limits)}
          </TabsTrigger>
          <TabsTrigger value="audit" className="flex-none px-3">
            {t(($) => $.page.tabs.audit)}
          </TabsTrigger>
        </TabsList>
      </Tabs>
      {tab === 'accounts' && <AccountsTab />}
      {tab === 'networks' && <NetworksTab />}
      {tab === 'limits' && <LimitsTab />}
      {tab === 'audit' && <AdminAuditTab />}
    </div>
  )
}
