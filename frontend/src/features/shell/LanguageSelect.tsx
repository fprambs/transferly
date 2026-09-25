import { useTranslation } from 'react-i18next'

import { NativeSelect } from '@/components/ui/native-select'
import { supportedLanguages } from '@/i18n'

/** Drop-down that switches the interface language; each language is named in itself. */
export function LanguageSelect() {
  const { t, i18n } = useTranslation()

  return (
    <div className="flex items-center gap-2">
      <label htmlFor="language" className="text-sm">
        {t(($) => $.language.label)}
      </label>
      <NativeSelect
        id="language"
        containerClassName="w-32"
        value={i18n.resolvedLanguage}
        onChange={(event) => void i18n.changeLanguage(event.target.value)}
      >
        {supportedLanguages.map((code) => (
          <option key={code} value={code}>
            {t(($) => $.language.names[code])}
          </option>
        ))}
      </NativeSelect>
    </div>
  )
}
