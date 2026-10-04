import { createHmac } from 'node:crypto'

import type { APIRequestContext, Locator, Page } from '@playwright/test'

import { expect, test } from '../fixtures.ts'

/** The well-known account of Azurite, the emulator of Azure Storage. */
const azurite = { account: 'devstoreaccount1', key: 'Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==' }

/** The account and the key of the fake of Azure Files of the test stack. */
const fakeFiles = { account: 'transferly', key: Buffer.from('transferly-azure-test-account-key').toString('base64') }

/** Creates a container in Azurite, signing the request with Shared Key as the documentation of Azure Storage describes. */
async function createContainer(request: APIRequestContext, name: string): Promise<void> {
  const date = new Date().toUTCString()
  const toSign = `PUT\n\n\n\n\n\n\n\n\n\n\n\nx-ms-date:${date}\nx-ms-version:2023-11-03\n/${azurite.account}/${azurite.account}/${name}\nrestype:container`
  const signature = createHmac('sha256', Buffer.from(azurite.key, 'base64')).update(toSign).digest('base64')
  const created = await request.put(`http://azurite:10000/${azurite.account}/${name}?restype=container`, {
    headers: { 'x-ms-date': date, 'x-ms-version': '2023-11-03', Authorization: `SharedKey ${azurite.account}:${signature}` },
  })
  expect(created.status()).toBe(201)
}

/** Offers connectors from the store in Administration, as an administrator does before anyone can use them. */
async function offer(page: Page, names: string[]): Promise<void> {
  await page.getByRole('button', { name: 'Account' }).click()
  await page.getByRole('menuitem', { name: 'Settings' }).click()
  await page.getByRole('navigation', { name: 'Settings' }).getByRole('button', { name: 'Administration', exact: true }).click()
  await page.getByRole('tab', { name: 'Connectors' }).click()
  for (const name of names) {
    const toggle = page.getByRole('listitem', { name }).getByRole('switch', { name: `Offer ${name}` })
    if (!(await toggle.isChecked())) {
      await toggle.click()
    }
  }
  await page.goto('/')
}

/** Fills the endpoint of an emulator on the Advanced tab of a site form: plain HTTP, its host and its port. */
async function emulator(dialog: Locator, host: string, port: string): Promise<void> {
  await dialog.getByRole('tab', { name: 'Advanced' }).click()
  await dialog.getByRole('checkbox', { name: 'Use HTTPS' }).click()
  await dialog.getByLabel('Endpoint').fill(host)
  await dialog.getByLabel('Port', { exact: true }).fill(port)
  await dialog.getByRole('tab', { name: 'General' }).click()
}

/** Creates a folder in the open folder, enters it, uploads a file, renames it and deletes it. */
async function workWithFiles(page: Page, folder: string, file: string): Promise<void> {
  const grid = page.getByRole('grid', { name: 'Files' })
  await page.getByRole('button', { name: 'New folder' }).click()
  await page.getByRole('dialog').getByLabel('Name').fill(folder)
  await page.getByRole('dialog').getByRole('button', { name: 'Create' }).click()
  await grid.getByRole('row', { name: new RegExp(folder) }).dblclick()
  await expect(page).toHaveURL(new RegExp(`${folder}$`))
  await page.getByLabel('Choose files to upload').setInputFiles({ name: file, mimeType: 'text/plain', buffer: Buffer.from('kept in azure\n') })
  const row = grid.getByRole('row', { name: new RegExp(file) })
  await expect(row).toBeVisible()
  await expect(row).toContainText('14 B')
  await row.click()
  await grid.press('F2')
  await page.getByRole('dialog').getByLabel('New name').fill(`renamed-${file}`)
  await page.getByRole('dialog').getByRole('button', { name: 'Rename' }).click()
  const renamed = grid.getByRole('row', { name: new RegExp(`renamed-${file}`) })
  await expect(renamed).toBeVisible()
  await renamed.click()
  await grid.press('Delete')
  await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click()
  await expect(renamed).toBeHidden()
}

/**
 * The Azure connectors (ADR-0025): Blob Storage against Azurite and Azure Files against the fake of the test stack.
 *
 * Scenarios:
 * - an administrator offers both; a Blob site is created with the account key and the endpoint of the emulator over plain HTTP, opens
 *   its container, and works with a folder and a file in it
 * - an Azure Files site opens its share, lists the folder already there, and works with a folder and a file in it
 */
test.describe('azure', () => {
  test('creates Azure sites and works with their files', async ({ shell, request }) => {
    test.setTimeout(120_000)
    const { page } = shell
    const stamp = Date.now()
    const container = `e2e-${stamp}`
    await createContainer(request, container)
    await offer(page, ['Azure Blob Storage', 'Azure Files'])

    await page.getByRole('button', { name: 'New site' }).first().click()
    let dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByRole('button', { name: /^Azure Blob Storage/ }).click()
    await dialog.getByLabel('Name', { exact: true }).fill(`Blobs ${stamp}`)
    await dialog.getByLabel('Storage account').fill(azurite.account)
    await dialog.getByLabel('Container').fill(container)
    await dialog.getByLabel('Account key').fill(azurite.key)
    await emulator(dialog, 'azurite', '10000')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()
    await page.getByRole('treeitem', { name: new RegExp(`Blobs ${stamp}`) }).dblclick()
    await expect(page.getByText('This folder is empty.')).toBeVisible()
    await workWithFiles(page, `folder-${stamp}`, `notes-${stamp}.txt`)

    await page.getByRole('button', { name: 'New site' }).first().click()
    dialog = page.getByRole('dialog', { name: 'New site' })
    await dialog.getByRole('button', { name: /^Azure Files/ }).click()
    await dialog.getByLabel('Name', { exact: true }).fill(`Share ${stamp}`)
    await dialog.getByLabel('Storage account').fill(fakeFiles.account)
    await dialog.getByLabel('Share').fill('share')
    await dialog.getByLabel('Account key').fill(fakeFiles.key)
    await emulator(dialog, 'fakeazure', '10002')
    await dialog.getByRole('button', { name: 'Create site' }).click()
    await expect(dialog).toBeHidden()
    await page.getByRole('treeitem', { name: new RegExp(`Share ${stamp}`) }).dblclick()
    const grid = page.getByRole('grid', { name: 'Files' })
    await grid.getByRole('row', { name: /Reports/ }).dblclick()
    await expect(grid.getByRole('row', { name: /q3\.csv/ })).toBeVisible()
    await page.getByRole('button', { name: 'Up one level' }).click()
    await workWithFiles(page, `folder-${stamp}`, `notes-${stamp}.txt`)
  })
})
