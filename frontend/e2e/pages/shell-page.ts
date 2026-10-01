import type { Locator, Page } from '@playwright/test'
import { choose } from '../select.ts'

/** A language the interface can be switched to, by its code. */
export type Language = 'en' | 'es'

/** Identifier of a theme the interface offers, such as `monokai`. */
export type Theme = string

/** Page object for the application shell: toolbar, sites sidebar, explorer and transfer queue, and the settings dialog. */
export class ShellPage {
  /** Level one heading with the product name. */
  readonly heading: Locator

  /** The Playwright page this object drives. */
  readonly page: Page

  /** Creates the page object for a Playwright page. */
  constructor(page: Page) {
    this.page = page
    this.heading = page.getByRole('heading', { level: 1 })
  }

  /** Opens the application root and waits for the shell to render. */
  async goto(): Promise<void> {
    await this.page.goto('/')
    await this.heading.waitFor()
  }

  /** Opens the settings dialog, runs an action there, and returns to the page the test was on. */
  private async inSettings(action: () => Promise<void>): Promise<void> {
    const back = this.page.url()
    await this.page.goto('/settings')
    await this.page.getByRole('radiogroup').waitFor()
    await action()
    await this.page.goto(back)
    await this.heading.waitFor()
  }

  /** Selects the interface language in the settings and returns to the page the test was on. */
  async setLanguage(language: Language): Promise<void> {
    await this.inSettings(async () => {
      await choose(this.page.locator('#language-setting'), language === 'es' ? 'Español' : 'English')
    })
  }

  /** Chooses a theme by its identifier in the settings, waits for the account to save it, and returns to the page the test was on. */
  async setTheme(theme: Theme): Promise<void> {
    await this.inSettings(async () => {
      const saved = this.page.waitForResponse((response) => response.url().endsWith('/api/v1/me') && response.request().method() === 'PATCH')
      await this.page.locator(`[data-theme-option="${theme}"]`).click()
      await saved
    })
  }
}
