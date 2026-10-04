import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { connector } from '@/test/handlers'
import { renderWithI18n } from '@/test/render'

import { ConnectorCatalog } from './ConnectorCatalog'

/**
 * Covers {@link ConnectorCatalog}: the tiles a new connection starts from (ADR-0019).
 *
 * Scenarios:
 * - each connector is a tile with its name and what it reaches; a connector without a description shows only its name
 * - connectors in beta say so, stable ones do not
 * - choosing a tile hands its type over
 */
describe('ConnectorCatalog', () => {
  it('lists the connectors as tiles and hands over the one chosen', async () => {
    const onChoose = vi.fn()
    const user = userEvent.setup()
    await renderWithI18n(<ConnectorCatalog connectors={[connector('sftp', 'SFTP'), connector('smb', 'SMB', true, 'beta')]} onChoose={onChoose} />)

    const catalog = screen.getByRole('list', { name: 'What do you want to connect to?' })
    const [sftp, smb] = within(catalog).getAllByRole('button')
    expect(sftp).toHaveTextContent('SFTPFiles over SSH, with keys, jump hosts and proxies.')
    expect(sftp).not.toHaveTextContent('Beta')
    expect(smb).toHaveTextContent(/^SMBBeta$/)

    await user.click(smb)
    expect(onChoose).toHaveBeenCalledWith('smb')
  })
})
