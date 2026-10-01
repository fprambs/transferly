import { Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Page, PageHeader } from '@/components/page/Page'
import { Checkbox } from '@/components/ui/checkbox'
import { Select } from '@/components/ui/select'
import { languages } from '@/i18n'
import { useUiStore } from '@/state/ui-store'

import { SecuritySection } from '../security/SecuritySection'

import { ThemeList } from './ThemeList'
import { useSavePreferences } from './use-save-preferences'
import { useThemeChoice } from './use-theme-choice'

/** Settings of the interface: the theme, following the system, the language, and what a double click on a file does; the theme, the language and the double click are kept with the account. */
export function SettingsPage() {
  const { t, i18n } = useTranslation('settings')
  const followSystem = useUiStore((state) => state.followSystem)
  const setFollowSystem = useUiStore((state) => state.setFollowSystem)
  const fileDoubleClick = useUiStore((state) => state.fileDoubleClick)
  const setFileDoubleClick = useUiStore((state) => state.setFileDoubleClick)
  const language = useSavePreferences()
  const doubleClick = useSavePreferences()
  const { choose, saveFailed } = useThemeChoice()

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <Page>
        <PageHeader icon={Settings} title={t(($) => $.title)} description={t(($) => $.description)} />
        <section aria-labelledby="appearance-title" className="flex flex-col gap-4 rounded-xl border bg-card px-5 py-4 shadow-xs">
          <div className="flex flex-col gap-1">
            <h3 id="appearance-title" className="text-sm font-semibold">
              {t(($) => $.appearance.title)}
            </h3>
            <p className="text-sm text-muted-foreground">{t(($) => $.appearance.description)}</p>
          </div>
          <div className="flex items-start gap-3">
            <Checkbox id="follow-system-setting" aria-labelledby="follow-system-setting-label" checked={followSystem} onCheckedChange={setFollowSystem} />
            <div className="flex flex-col text-sm">
              <span id="follow-system-setting-label" className="font-medium">
                {t(($) => $.appearance.followSystem)}
              </span>
              <span className="text-muted-foreground">{t(($) => $.appearance.followSystemHint)}</span>
            </div>
          </div>
          <ThemeList onChoose={choose} />
          {saveFailed ? (
            <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {t(($) => $.appearance.saveFailed)}
            </p>
          ) : null}
        </section>
        <div className="grid gap-4 md:grid-cols-2">
          <section aria-labelledby="language-title" className="flex flex-col gap-3 rounded-xl border bg-card px-5 py-4 shadow-xs">
            <h3 id="language-title" className="text-sm font-semibold">
              {t(($) => $.language.title)}
            </h3>
            <label htmlFor="language-setting" className="text-sm text-muted-foreground">
              {t(($) => $.language.label)}
            </label>
            <Select
              id="language-setting"
              value={i18n.resolvedLanguage as string}
              options={languages.map((language) => ({ value: language.code, label: language.name }))}
              onValueChange={(locale) => {
                void i18n.changeLanguage(locale)
                void language.save({ locale })
              }}
            />
            {language.saveFailed ? (
              <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {t(($) => $.language.saveFailed)}
              </p>
            ) : null}
          </section>
          <section aria-labelledby="explorer-title" className="flex flex-col gap-3 rounded-xl border bg-card px-5 py-4 shadow-xs">
            <h3 id="explorer-title" className="text-sm font-semibold">
              {t(($) => $.explorer.title)}
            </h3>
            <label htmlFor="file-double-click-setting" className="text-sm text-muted-foreground">
              {t(($) => $.explorer.doubleClick)}
            </label>
            <Select
              id="file-double-click-setting"
              value={fileDoubleClick}
              options={[
                { value: 'download', label: t(($) => $.explorer.download) },
                { value: 'edit', label: t(($) => $.explorer.edit) },
                { value: 'preview', label: t(($) => $.explorer.preview) },
              ]}
              onValueChange={(value) => {
                const action = value === 'edit' || value === 'preview' ? value : 'download'
                setFileDoubleClick(action)
                void doubleClick.save({ fileDoubleClick: action })
              }}
            />
            <p className="text-xs text-muted-foreground">{t(($) => $.explorer.doubleClickHint)}</p>
            {doubleClick.saveFailed ? (
              <p role="alert" className="rounded-md bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {t(($) => $.explorer.saveFailed)}
              </p>
            ) : null}
          </section>
        </div>
        <SecuritySection />
      </Page>
    </div>
  )
}
