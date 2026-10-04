import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ConnectorIcon } from './ConnectorIcon'

/**
 * Covers {@link ConnectorIcon}.
 *
 * Scenarios:
 * - each icon a built-in connector names is drawn from the brand kit, with its accent in the brand color, and is decorative
 * - an icon the interface does not know gets the generic connector icon
 */
describe('ConnectorIcon', () => {
  it.each(['sftp', 'ftps', 'cloud', 'gcs', 'gdrive', 'azblob', 'azfiles'])('draws the %s icon', (icon) => {
    const { container } = render(<ConnectorIcon icon={icon} className="size-6" />)
    const svg = container.querySelector('svg')
    expect(svg).toHaveAttribute('data-icon', icon)
    expect(svg).toHaveAttribute('aria-hidden', 'true')
    expect(svg).toHaveClass('size-6')
    expect(svg?.querySelector('.text-primary')).not.toBeNull()
  })

  it('draws a generic icon for an unknown one', () => {
    const { container } = render(<ConnectorIcon icon="bucket" />)
    expect(container.querySelector('svg')).toHaveAttribute('data-icon', 'connector')
  })
})
