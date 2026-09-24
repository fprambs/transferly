import type { Locator, Page } from '@playwright/test'

/** A language the interface can be switched to, by its code. */
export type Language = 'en' | 'es'

/** A color scheme choice offered by the theme buttons. */
export type Theme = 'light' | 'dark' | 'system'

/** Text of each theme button per interface language. */
const themeLabels: Record<Language, Record<Theme, string>> = {
  en: { light: 'Light', dark: 'Dark', system: 'System' },
  es: { light: 'Claro', dark: 'Oscuro', system: 'Sistema' },
}

/** Page object for the application shell: toolbar, sites sidebar, file panes and transfer queue. */
export class ShellPage {
  /** Level one heading with the product name. */
  readonly heading: Locator
  /** Language drop-down of the toolbar. */
  readonly languageSelect: Locator
  /** Group of theme buttons. */
  readonly themeGroup: Locator

  /** The Playwright page this object drives. */
  readonly page: Page

  /** Creates the page object for a Playwright page. */
  constructor(page: Page) {
    this.page = page
    this.heading = page.getByRole('heading', { level: 1 })
    this.languageSelect = page.locator('#language')
    this.themeGroup = page.getByRole('group')
  }

  /** Opens the application root and waits for the shell to render. */
  async goto(): Promise<void> {
    await this.page.goto('/')
    await this.heading.waitFor()
  }

  /** Selects a language in the toolbar. */
  async setLanguage(language: Language): Promise<void> {
    await this.languageSelect.selectOption(language)
  }

  /** Presses one of the theme buttons, using its text in the language currently shown. */
  async setTheme(theme: Theme): Promise<void> {
    const language = (await this.page.locator('html').getAttribute('lang')) as Language
    await this.themeGroup.getByRole('button', { name: themeLabels[language][theme] }).click()
  }
}
