import { useTranslation } from 'react-i18next'

import { Checkbox } from '@/components/ui/checkbox'
import { NativeSelect } from '@/components/ui/native-select'
import { themes } from '@/lib/theme'
import { useUiStore } from '@/state/ui-store'

/** Drop-down that chooses the interface theme, with the option to follow the operating system. */
export function ThemeSelect() {
  const { t } = useTranslation('shell')
  const themeId = useUiStore((state) => state.themeId)
  const followSystem = useUiStore((state) => state.followSystem)
  const setTheme = useUiStore((state) => state.setTheme)
  const setFollowSystem = useUiStore((state) => state.setFollowSystem)

  return (
    <div className="flex items-center gap-3">
      <label htmlFor="theme" className="text-sm">
        {t(($) => $.theme.label)}
      </label>
      <NativeSelect
        id="theme"
        containerClassName="w-40"
        value={themeId}
        disabled={followSystem}
        onChange={(event) => setTheme(event.target.value)}
      >
        {(['dark', 'light'] as const).map((scheme) => (
          <optgroup key={scheme} label={t(($) => $.theme.groups[scheme])}>
            {themes
              .filter((theme) => theme.scheme === scheme)
              .map((theme) => (
                <option key={theme.id} value={theme.id}>
                  {theme.name}
                </option>
              ))}
          </optgroup>
        ))}
      </NativeSelect>
      <div className="flex items-center gap-2 text-sm">
        <Checkbox id="follow-system" aria-labelledby="follow-system-label" checked={followSystem} onCheckedChange={setFollowSystem} />
        <span id="follow-system-label">{t(($) => $.theme.followSystem)}</span>
      </div>
    </div>
  )
}
