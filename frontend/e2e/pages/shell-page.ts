import type { Locator, Page } from '@playwright/test'

/** A language the interface can be switched to, by its code. */
export type Language = 'en' | 'es'

/** Identifier of a theme the interface offers, such as `monokai`. */
export type Theme = string

/** Page object for the application shell: toolbar, sites sidebar, file panes and transfer queue. */
export class ShellPage {
  /** Level one heading with the product name. */
  readonly heading: Locator
  /** Language drop-down of the toolbar. */
  readonly languageSelect: Locator
  /** Theme drop-down of the toolbar. */
  readonly themeSelect: Locator

  /** The Playwright page this object drives. */
  readonly page: Page

  /** Creates the page object for a Playwright page. */
  constructor(page: Page) {
    this.page = page
    this.heading = page.getByRole('heading', { level: 1 })
    this.languageSelect = page.locator('#language')
    this.themeSelect = page.locator('#theme')
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

  /** Chooses a theme in the toolbar by its identifier. */
  async setTheme(theme: Theme): Promise<void> {
    await this.themeSelect.selectOption(theme)
  }
}
