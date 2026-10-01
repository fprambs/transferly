import { ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PageHeader } from '@/components/page/Page'
import { Loading } from '@/components/ui/loading'
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
    return <Loading label={t(($) => $.page.loading)} className="p-7" />
  }
  if (session.user.role !== 'admin') {
    return <p className="p-7 text-sm text-muted-foreground">{t(($) => $.page.noAccess)}</p>
  }
  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-7">
      <PageHeader icon={ShieldCheck} title={t(($) => $.page.title)} description={t(($) => $.page.subtitle)}>
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
      </PageHeader>
      {tab === 'accounts' && <AccountsTab />}
      {tab === 'networks' && <NetworksTab />}
      {tab === 'limits' && <LimitsTab />}
      {tab === 'audit' && <AdminAuditTab />}
    </div>
  )
}
