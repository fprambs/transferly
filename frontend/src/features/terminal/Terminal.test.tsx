import { act, cleanup, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { FakeSocket, fakeSocketTransport } from '@/test/fake-socket'
import { renderWithProviders } from '@/test/render'

import { TerminalPanel } from './TerminalPanel'
import { TerminalView } from './TerminalView'

/** What the fake xterm.js recorded, for the tests to play the person typing and resizing. */
const screenLog = vi.hoisted(() => ({
  written: [] as string[],
  typed: undefined as ((data: string) => void) | undefined,
  resized: undefined as ((size: { cols: number; rows: number }) => void) | undefined,
  disposed: 0,
  fitted: 0,
}))

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80
    rows = 24
    loadAddon(): void {}
    open(): void {}
    focus(): void {}
    write(data: string | Uint8Array): void {
      screenLog.written.push(typeof data === 'string' ? data : new TextDecoder().decode(data))
    }
    onData(listener: (data: string) => void) {
      screenLog.typed = listener
      return { dispose: () => undefined }
    }
    onResize(listener: (size: { cols: number; rows: number }) => void) {
      screenLog.resized = listener
      return { dispose: () => undefined }
    }
    dispose(): void {
      screenLog.disposed++
    }
  },
}))

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit(): void {
      screenLog.fitted++
    }
  },
}))

/** The socket of the terminal opened last. */
function socket(): FakeSocket {
  return FakeSocket.opened.at(-1) as FakeSocket
}

/**
 * Covers {@link TerminalView} and {@link TerminalPanel} (ADR-0027).
 *
 * Scenarios:
 * - a terminal opens the WebSocket of the site with its folder and screen, or without a folder for the home path, and says it is
 *   opening until the server accepts it
 * - the screen fits the panel when it opens and whenever the panel changes size
 * - what the shell prints is drawn, what is typed and every resize go to the server once it is open, and nothing before
 * - when the shell ends its status is written and the connection closing is not a failure; text messages it does not know are ignored
 * - a connection that drops says so, centered, and opens again on request; closing the terminal closes its socket and its screen
 * - the panel names each terminal and its folder, shows the one chosen, opens another, closes one or all
 */
describe('terminal', () => {
  beforeEach(() => {
    FakeSocket.reset()
    screenLog.written = []
    screenLog.disposed = 0
    screenLog.fitted = 0
    vi.stubGlobal(
      'ResizeObserver',
      class {
        readonly changed: () => void
        constructor(changed: () => void) {
          this.changed = changed
        }
        observe(): void {
          this.changed()
        }
        disconnect(): void {}
      },
    )
  })
  afterEach(() => {
    FakeSocket.reset()
    vi.unstubAllGlobals()
  })

  it('relays a shell over its socket', async () => {
    await renderWithProviders(<TerminalView workspaceId="w" siteId="s" path="/srv" />, 'en', fakeSocketTransport())
    expect(socket().url).toBe(`ws://${window.location.host}/api/v1/workspaces/w/sites/s/terminal?path=%2Fsrv&cols=80&rows=24`)
    expect(socket().binaryType).toBe('arraybuffer')
    expect(screenLog.fitted).toBe(2)
    expect(screen.getByText('Opening the terminal…')).toBeInTheDocument()
    act(() => screenLog.typed?.('early'))
    act(() => screenLog.resized?.({ cols: 1, rows: 1 }))
    expect(socket().sent).toEqual([])

    act(() => socket().open())
    expect(screen.queryByText('Opening the terminal…')).not.toBeInTheDocument()
    act(() => socket().receive(new TextEncoder().encode('$ ').buffer))
    expect(screenLog.written).toEqual(['$ '])
    act(() => screenLog.typed?.('ls\r'))
    act(() => screenLog.resized?.({ cols: 120, rows: 40 }))
    expect(new TextDecoder().decode(socket().sent[0] as Uint8Array)).toBe('ls\r')
    expect(socket().sent[1]).toBe('{"type":"resize","cols":120,"rows":40}')

    act(() => socket().receive('not json'))
    act(() => socket().receive('{"type":"other"}'))
    act(() => socket().receive('{"type":"exit","code":3}'))
    expect(screenLog.written.at(-1)).toContain('[The shell ended with status 3]')
    act(() => socket().drop())
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    cleanup()
    expect(socket().closed).toBe(true)
    expect(screenLog.disposed).toBe(1)
  })

  it('says a dropped terminal is closed and opens it again', async () => {
    const user = userEvent.setup()
    await renderWithProviders(<TerminalView workspaceId="w" siteId="s" path="~" />, 'en', fakeSocketTransport())
    expect(socket().url).toBe(`ws://${window.location.host}/api/v1/workspaces/w/sites/s/terminal?cols=80&rows=24`)
    act(() => socket().drop())
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('The terminal is closed')
    await user.click(within(alert).getByRole('button', { name: 'Open again' }))
    expect(FakeSocket.opened).toHaveLength(2)
    expect(screen.getByText('Opening the terminal…')).toBeInTheDocument()
  })

  it('lists its terminals in tabs', async () => {
    const onActivate = vi.fn()
    const onNew = vi.fn()
    const onClose = vi.fn()
    const onCloseAll = vi.fn()
    const user = userEvent.setup()
    await renderWithProviders(
      <TerminalPanel
        workspaceId="w"
        siteId="s"
        terminals={[
          { id: 1, path: '/srv' },
          { id: 2, path: '/var/log' },
        ]}
        active={2}
        onActivate={onActivate}
        onNew={onNew}
        onClose={onClose}
        onCloseAll={onCloseAll}
      />,
      'en',
      fakeSocketTransport(),
    )
    const tabs = screen.getAllByRole('tab')
    expect(tabs[0]).toHaveTextContent('Terminal 1/srv')
    expect(tabs[1]).toHaveAttribute('aria-selected', 'true')
    expect(screen.getAllByRole('tabpanel', { hidden: true })[0]).not.toBeVisible()
    await user.click(tabs[0])
    await user.click(screen.getByRole('button', { name: 'Close Terminal 2' }))
    await user.click(screen.getByRole('button', { name: 'New terminal in this folder' }))
    await user.click(screen.getByRole('button', { name: 'Close the terminals' }))
    expect(onActivate).toHaveBeenCalledWith(1)
    expect(onClose).toHaveBeenCalledWith(2)
    expect(onNew).toHaveBeenCalled()
    expect(onCloseAll).toHaveBeenCalled()
  })
})
