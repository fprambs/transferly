import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { act } from 'react'
import { describe, expect, it } from 'vitest'

import { useUiStore } from '@/state/ui-store'
import { textSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import { TextViewer } from './TextViewer'

/** Renders the code view of a text directly, the way the viewer does once the file is read. */
async function renderViewer(name: string, text: string) {
  const { render } = await import('@testing-library/react')
  const { CodeView } = await import('./CodeView')
  return render(<CodeView name={name} text={text} />)
}

/**
 * Covers {@link TextViewer} and the {@link CodeView} it draws (ADR-0018).
 *
 * Scenarios:
 * - the text is shown read-only with line numbers, whatever its language
 * - long lines wrap on demand and stop wrapping again
 * - a change of the theme follows without rebuilding the view
 * - a view closed before the language of its file loaded is not built
 */
describe('TextViewer', () => {
  it('shows text read-only and wraps on demand', async () => {
    await renderWithProviders(<TextViewer source={textSource('main.go', 'package main\n\nfunc main() {}\n')} kind="text" />)
    const view = await screen.findByLabelText('Contents of main.go')
    await waitFor(() => expect(view).toHaveTextContent('package main'))
    expect(view.querySelector('.cm-content')).toHaveAttribute('contenteditable', 'false')

    const wrap = screen.getByRole('button', { name: 'Wrap lines' })
    const user = userEvent.setup()
    await user.click(wrap)
    expect(wrap).toHaveAttribute('aria-pressed', 'true')
    expect(view.querySelector('.cm-lineWrapping')).not.toBeNull()
    await user.click(wrap)
    expect(view.querySelector('.cm-lineWrapping')).toBeNull()

    act(() => useUiStore.setState({ themeId: 'github-light', followSystem: false }))
    expect(view).toHaveTextContent('package main')
    act(() => useUiStore.setState({ themeId: 'sentinel-dark' }))
    expect(view).toHaveTextContent('package main')
  })

  it('stops building a view that closed before its language loaded', async () => {
    const { unmount } = await renderViewer('late.ts', 'let x = 1')
    unmount()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(document.querySelector('.cm-editor')).toBeNull()
  })

  it('shows plain text of an unknown language in a light theme', async () => {
    useUiStore.setState({ themeId: 'github-light', followSystem: false })
    await renderWithProviders(<TextViewer source={textSource('notes', 'just words')} kind="text" />)
    const view = await screen.findByLabelText('Contents of notes')
    await waitFor(() => expect(view).toHaveTextContent('just words'))
  })
})
