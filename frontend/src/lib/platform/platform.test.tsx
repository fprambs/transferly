import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PlatformProvider, usePlatform, webPlatform, type Platform } from './index'

/** Renders the platform kind and local file system capability. */
function Probe() {
  const platform = usePlatform()
  return (
    <p>
      {platform.kind} {platform.hasLocalFs ? 'with local files' : 'without local files'}
    </p>
  )
}

/**
 * Covers {@link usePlatform} and {@link PlatformProvider}.
 *
 * Scenarios:
 * - without a provider the browser capabilities apply
 * - a provider overrides the capabilities for its subtree
 */
describe('usePlatform', () => {
  it('defaults to the browser capabilities', () => {
    render(<Probe />)

    expect(webPlatform.kind).toBe('web')
    expect(screen.getByText('web without local files')).toBeInTheDocument()
  })

  it('uses the capabilities given by the provider', () => {
    const desktop: Platform = { kind: 'desktop', hasLocalFs: true, canPickFolders: true }
    render(
      <PlatformProvider platform={desktop}>
        <Probe />
      </PlatformProvider>,
    )

    expect(screen.getByText('desktop with local files')).toBeInTheDocument()
  })
})
