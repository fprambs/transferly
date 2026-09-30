import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { EmailViewer } from './EmailViewer'

/** Reads a mail sample, shared with the test stack; the tests run from the frontend folder. */
function sample(name: string): Uint8Array {
  return new Uint8Array(readFileSync(join(process.cwd(), '../testenv/preview-samples/mail', name)))
}

/** Shows a message. */
async function show(name: string, bytes = sample(name)) {
  await renderWithProviders(<EmailViewer source={bytesSource(name, bytes)} kind="email" />)
  return userEvent.setup()
}

/**
 * Covers {@link EmailViewer} (D2.7).
 *
 * Scenarios:
 * - a newsletter shows its subject, addresses and date, its HTML in the isolated page, its plain text and header lines, and attachments
 *   that open with their own viewer and go back, or save on the computer
 * - a HTML body the page cannot draw falls back to the text, and a hostile message says its remote content was not loaded
 * - a message without HTML shows its text, one without a subject says so, one whose date cannot be read shows it as written, and a file
 *   that is not a message says it cannot be read
 */
describe('EmailViewer', () => {
  it('shows a newsletter', async () => {
    const user = await show('newsletter.eml')
    expect(await screen.findByRole('heading', { name: 'Informe de septiembre · 📊 resultados' })).toBeInTheDocument()
    expect(screen.getByText('Sentinel Samples <samples@example.com>')).toBeInTheDocument()
    expect(screen.getByText('Carla <carla@example.com>')).toBeInTheDocument()
    expect(screen.getByText(/2026/)).toBeInTheDocument()
    expect(screen.getByTitle('Message newsletter.eml')).toBeInTheDocument()
    expect(screen.queryByText(/were not loaded/)).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Plain text' }))
    expect(screen.getByText(/Adjunto el informe/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Headers' }))
    expect(screen.getByText(/Message-ID:/)).toBeInTheDocument()

    URL.createObjectURL = vi.fn(() => 'blob:attachment')
    URL.revokeObjectURL = vi.fn()
    const clicked: string[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicked.push(this.download)
    })
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await user.click(screen.getByRole('button', { name: 'Save notas.txt' }))
    expect(clicked).toEqual(['notas.txt'])
    act(() => void vi.advanceTimersByTime(60_000))
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:attachment')
    vi.useRealTimers()

    const list = screen.getByRole('list', { name: 'Attachments' })
    await user.click(within(list).getByText('notas.txt'))
    expect(await screen.findByLabelText('Contents of notas.txt')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'newsletter.eml' }))
    expect(screen.getByRole('list', { name: 'Attachments' })).toBeInTheDocument()
  })

  it('falls back to the text and warns about remote content', async () => {
    await show('hostile.eml')
    expect(await screen.findByText('Remote images and content of this message were not loaded.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '<img src=x onerror=alert(1)> urgent' })).toBeInTheDocument()
    const frame = screen.getByTitle('Message hostile.eml') as HTMLIFrameElement
    act(() => window.dispatchEvent(new MessageEvent('message', { data: { type: 'sentinel:failed', reason: 'render' }, source: frame.contentWindow })))
    expect(await screen.findByText('See the HTML part.')).toBeInTheDocument()
  })

  it('handles plain, odd and foreign files', async () => {
    const plain = ['From: a@b', 'Date: sometime', '', 'plain body'].join('\r\n')
    await show('plain.eml', new TextEncoder().encode(plain))
    expect(await screen.findByRole('heading', { name: '(no subject)' })).toBeInTheDocument()
    expect(screen.getByText('sometime')).toBeInTheDocument()
    expect(screen.getByText(/plain body/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Plain text' })).not.toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Attachments' })).not.toBeInTheDocument()
    const bare = ['Subject: bare', '', ''].join('\r\n')
    await show('bare.eml', new TextEncoder().encode(bare))
    expect(await screen.findByRole('heading', { name: 'bare' })).toBeInTheDocument()
  })

  it('says when a message cannot be read', async () => {
    const readMail = vi.spyOn(await import('./mail'), 'readMail').mockRejectedValue(new Error('broken'))
    await show('broken.eml', new TextEncoder().encode('x'))
    expect(await screen.findByText('This message cannot be read')).toBeInTheDocument()
    readMail.mockRestore()
  })
})
