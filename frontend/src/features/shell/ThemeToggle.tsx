import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { themePreferences } from '@/lib/theme'
import { useUiStore } from '@/state/ui-store'

/** Group of buttons that selects the light, dark or system color scheme. */
export function ThemeToggle() {
  const { t } = useTranslation('shell')
  const theme = useUiStore((state) => state.theme)
  const setTheme = useUiStore((state) => state.setTheme)

  return (
    <div role="group" aria-label={t(($) => $.theme.label)} className="flex gap-1">
      {themePreferences.map((preference) => (
        <Button
          key={preference}
          size="sm"
          variant={theme === preference ? 'default' : 'outline'}
          aria-pressed={theme === preference}
          onClick={() => setTheme(preference)}
        >
          {t(($) => $.theme[preference])}
        </Button>
      ))}
    </div>
  )
}
