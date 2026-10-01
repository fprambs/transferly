import { act, fireEvent, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import type { ConnectionLogEntry } from '@/lib/transport'
import { useUiStore } from '@/state/ui-store'
import { renderWithProviders } from '@/test/render'

import { ConnectionLog } from './ConnectionLog'
import { logKey, maxLogLines, useConnectionLog } from './connection-log-store'

/** An entry of the log of site s1 with the given step, parameters and level. */
function entry(step: string, params: Record<string, string> = {}, level: ConnectionLogEntry['level'] = 'info', siteId = 's1'): ConnectionLogEntry {
  return { workspaceId: 'w1', siteId, time: '2026-09-28T15:04:05Z', level, step, params }
}

/** Adds entries to the log. */
function add(...entries: ConnectionLogEntry[]): void {
  act(() => {
    for (const e of entries) {
      useConnectionLog.getState().add(e)
    }
  })
}

/** The texts of the lines shown, without their times. */
function shown(): string[] {
  return within(screen.getByRole('list', { name: 'Connection log' }))
    .getAllByRole('listitem')
    .map((item) => item.querySelector('span')?.textContent ?? '')
}

/**
 * Covers {@link ConnectionLog} and its store (ADR-0011).
 *
 * Scenarios:
 * - every step is written in words with its values: how a connection is reached, the key or certificate verdict, clear text, what the server
 *   asks, the account and its logon type, listings, and failures with the message of their code; jump host steps say so; unknown steps and logon types show as they came
 * - each site has its own log, which keeps its newest lines only, and the panel says when it is empty
 * - a connection that starts after the site was disconnected starts the log again, while one that starts after a failure keeps the failure
 * - lines of problems are colored, steps that went right are green and the end of a session is muted, times use the clock of the browser, and the newest line stays in view unless the user scrolled up
 * - the panel can be collapsed and opened, copied as text and cleared
 */
describe('ConnectionLog', () => {
  beforeEach(() => {
    useConnectionLog.setState({ lines: {}, next: 1 })
    useUiStore.setState({ logOpen: true })
  })

  it('writes every step in words', async () => {
    await renderWithProviders(<ConnectionLog workspaceId="w1" siteId="s1" />)
    add(
      entry('connecting', { host: 'h', port: '22', via: 'direct' }),
      entry('connecting', { host: 'b', port: '2222', via: 'proxy', proxy: 'p:1080', hop: 'jump' }),
      entry('connecting', { host: 'h', port: '22', via: 'jump' }),
      entry('connected', { address: '10.0.0.1:22' }),
      entry('host_key_verified', { keyType: 'ssh-ed25519', fingerprint: 'SHA256:k' }),
      entry('host_key_verified', { keyType: 'tls-certificate', fingerprint: 'SHA256:c' }),
      entry('host_key_unknown', { keyType: 'ssh-rsa', fingerprint: 'SHA256:u' }, 'warn'),
      entry('host_key_changed', { keyType: 'tls-certificate', fingerprint: 'SHA256:x' }, 'error'),
      entry('plain_text', {}, 'warn'),
      entry('authenticated', { method: 'key', username: 'deploy' }),
      entry('authenticated', { method: 'kerberos', username: 'deploy' }),
      entry('prompted', { name: 'Two steps' }),
      entry('prompted', { name: '' }),
      entry('authenticated', { method: 'interactive', username: 'deploy' }),
      entry('listing', { path: '/srv' }),
      entry('listed', { path: '/srv', count: '4' }),
      entry('failed', { path: '/x', code: 'path_not_found' }, 'error'),
      entry('failed', { path: '/x', code: 'something_new' }, 'error'),
      entry('resolving', { host: 'h' }),
      entry('disconnected', {}),
    )

    expect(shown()).toEqual([
      'Connecting to h:22…',
      'Jump host: Connecting to b:2222 through the proxy p:1080…',
      'Connecting to h:22 through the jump host…',
      'Connected to 10.0.0.1:22.',
      'Server key verified: ssh-ed25519 SHA256:k.',
      'Server certificate verified: SHA256:c.',
      'The server key is not trusted yet: ssh-rsa SHA256:u. Confirm it to go on.',
      'The server certificate changed: SHA256:x. The connection was refused.',
      'The server offers no TLS: going on in clear text, the password included.',
      'Signed in as deploy (key file).',
      'Signed in as deploy (kerberos).',
      'The server asks: Two steps. Waiting for your answer…',
      'The server asks for an answer. Waiting for it…',
      'Signed in as deploy (interactive).',
      'Listing /srv…',
      'Listed /srv: 4 entries.',
      expect.stringMatching(/^Failed: .+ \(path_not_found\)$/),
      expect.stringMatching(/^Failed: .+ \(something_new\)$/),
      'resolving',
      'Disconnected.',
    ])
    const items = within(screen.getByRole('list', { name: 'Connection log' })).getAllByRole('listitem')
    expect(items[6]).toHaveClass('text-warn')
    expect(items[7]).toHaveClass('text-destructive')
    expect(items[0]).toHaveClass('text-foreground')
    expect(items[3]).toHaveClass('text-ok')
    expect(items.at(-1)).toHaveClass('text-muted-foreground')
    expect(within(items[0]).getByText(/\d\d:\d\d:\d\d/)).toHaveAttribute('dateTime', '2026-09-28T15:04:05Z')
  })

  it('keeps a log per site, with its newest lines only', async () => {
    await renderWithProviders(<ConnectionLog workspaceId="w1" siteId="s1" />)
    expect(screen.getByText('The steps of the connection show here.')).toBeInTheDocument()

    add(entry('connected', { address: 'other' }, 'info', 's2'))
    expect(screen.getByText('The steps of the connection show here.')).toBeInTheDocument()

    act(() => {
      for (let i = 0; i < maxLogLines + 5; i++) {
        useConnectionLog.getState().add(entry('listing', { path: `/${i}` }))
      }
    })
    const lines = useConnectionLog.getState().lines[logKey('w1', 's1')]
    expect(lines).toHaveLength(maxLogLines)
    expect(lines[0].params.path).toBe('/5')
    expect(screen.getByRole('button', { name: /Log/ })).toHaveTextContent(String(maxLogLines))
  })

  it('starts again when a disconnected site connects, and keeps a failure above a new attempt', async () => {
    await renderWithProviders(<ConnectionLog workspaceId="w1" siteId="s1" />)
    add(entry('connecting', { host: 'prod', port: '22' }), entry('failed', { code: 'remote_unavailable' }, 'error'), entry('connecting', { host: 'prod', port: '22' }))
    expect(shown()).toHaveLength(3)

    add(entry('connected', { address: '10.0.0.1:22' }), entry('disconnected'))
    expect(shown()).toHaveLength(5)
    add(entry('connecting', { host: 'prod', port: '22' }))
    expect(shown()).toEqual(['Connecting to prod:22…'])
  })

  it('follows the newest line unless the user scrolled up', async () => {
    await renderWithProviders(<ConnectionLog workspaceId="w1" siteId="s1" />)
    const list = screen.getByRole('list', { name: 'Connection log' })
    Object.defineProperty(list, 'scrollHeight', { configurable: true, value: 500 })
    Object.defineProperty(list, 'clientHeight', { configurable: true, value: 100 })

    add(entry('listing', { path: '/a' }))
    expect(list.scrollTop).toBe(500)

    list.scrollTop = 100
    fireEvent.scroll(list)
    add(entry('listing', { path: '/b' }))
    expect(list.scrollTop).toBe(100)

    list.scrollTop = 400
    fireEvent.scroll(list)
    add(entry('listing', { path: '/c' }))
    expect(list.scrollTop).toBe(500)
  })

  it('collapses, copies and clears', async () => {
    const written: string[] = []
    const user = userEvent.setup()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: (text: string) => (written.push(text), Promise.resolve()) } })
    await renderWithProviders(<ConnectionLog workspaceId="w1" siteId="s1" />)
    expect(screen.getByRole('button', { name: 'Copy the log' })).toBeDisabled()
    add(entry('connected', { address: '10.0.0.1:22' }), entry('listing', { path: '/' }))

    await user.click(screen.getByRole('button', { name: 'Copy the log' }))
    expect(written[0]).toMatch(/^\d\d:\d\d:\d\d {2}Connected to 10\.0\.0\.1:22\.\n\d\d:\d\d:\d\d {2}Listing \/…$/)
    expect(screen.getByRole('status')).toHaveTextContent('Copied')

    await user.click(screen.getByRole('button', { name: /Log/ }))
    expect(screen.queryByRole('list', { name: 'Connection log' })).not.toBeInTheDocument()
    expect(useUiStore.getState().logOpen).toBe(false)
    await user.click(screen.getByRole('button', { name: /Log/ }))
    expect(screen.getByRole('list', { name: 'Connection log' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Clear the log' }))
    expect(screen.getByText('The steps of the connection show here.')).toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear the log' })).toBeDisabled()
  })
})
