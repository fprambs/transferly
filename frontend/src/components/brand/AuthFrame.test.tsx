import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderWithI18n } from '@/test/render'

import { AuthFrame } from './AuthFrame'

/**
 * Covers {@link AuthFrame}.
 *
 * Scenarios:
 * - the brand panel names the product, says its tagline and the three things it does, in the language of the interface
 * - the card holds the page and takes the width it is given, narrow by default
 */
describe('AuthFrame', () => {
  it('tells the brand beside the card', async () => {
    await renderWithI18n(
      <AuthFrame>
        <p>Form</p>
      </AuthFrame>,
    )

    expect(screen.getByText('Transferly')).toBeInTheDocument()
    expect(screen.getByText('Seamless data flow between all your connections.')).toBeInTheDocument()
    expect(screen.getAllByRole('listitem').map((item) => item.textContent)).toEqual([
      'SFTP, FTP and FTPS in the browser',
      'Credentials sealed in an encrypted vault',
      'Transfers that resume where they stopped',
    ])
    expect(screen.getByText('Form').parentElement).toHaveClass('max-w-sm')
  })

  it('speaks Spanish and takes a wider card', async () => {
    await renderWithI18n(
      <AuthFrame width="max-w-md">
        <p>Formulario</p>
      </AuthFrame>,
      'es',
    )

    expect(screen.getByText('Tus datos fluyen sin fricción entre todas tus conexiones.')).toBeInTheDocument()
    expect(screen.getByText('Formulario').parentElement).toHaveClass('max-w-md')
  })
})
