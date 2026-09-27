import { useTranslation } from 'react-i18next'

import { Checkbox } from '@/components/ui/checkbox'
import { NativeSelect } from '@/components/ui/native-select'
import { supportedLanguages } from '@/i18n'
import { useUiStore } from '@/state/ui-store'

import { SecuritySection } from '../security/SecuritySection'

import { ThemeList } from './ThemeList'
import { useThemeChoice } from './use-theme-choice'

/** Settings of the interface: the theme, following the system, and the language. */
export function SettingsPage() {
  const { t, i18n } = useTranslation('settings')
  const { t: common } = useTranslation()
  const followSystem = useUiStore((state) => state.followSystem)
  const setFollowSystem = useUiStore((state) => state.setFollowSystem)
  const { choose, saveFailed } = useThemeChoice()

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-8 overflow-y-auto p-6">
      <h2 className="text-lg font-semibold tracking-tight">{t(($) => $.title)}</h2>
      <section aria-labelledby="appearance-title" className="flex max-w-4xl flex-col gap-4">
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
      <section aria-labelledby="language-title" className="flex max-w-sm flex-col gap-3">
        <h3 id="language-title" className="text-sm font-semibold">
          {t(($) => $.language.title)}
        </h3>
        <label htmlFor="language-setting" className="text-sm text-muted-foreground">
          {t(($) => $.language.label)}
        </label>
        <NativeSelect id="language-setting" value={i18n.resolvedLanguage} onChange={(event) => void i18n.changeLanguage(event.target.value)}>
          {supportedLanguages.map((code) => (
            <option key={code} value={code}>
              {common(($) => $.language.names[code])}
            </option>
          ))}
        </NativeSelect>
      </section>
      <SecuritySection />
    </div>
  )
}
