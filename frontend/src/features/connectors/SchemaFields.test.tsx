import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm, useWatch } from 'react-hook-form'
import { describe, expect, it } from 'vitest'

import { connector } from '@/test/handlers'
import { renderWithProviders } from '@/test/render'
import { choose, optionsOf } from '@/test/select'

import type { ConnectorField } from './schema'
import { SchemaFields, type SchemaValues } from './SchemaFields'

/** A connector with fields of the kinds and shapes the built-in ones lack, none of them translated. */
const sample = {
  ...connector('sample', 'Sample'),
  fields: [
    { name: 'endpoint', kind: 'host', group: 'connection', required: true, default: 'storage.example.com' },
    { name: 'mode', kind: 'choice', group: 'connection', required: false, choices: ['fast', 'safe'] },
    { name: 'flavor', kind: 'choice', group: 'connection', required: true },
    { name: 'secure', kind: 'boolean', group: 'connection', required: false, default: 'false' },
    { name: 'tls', kind: 'boolean', group: 'connection', required: true, default: 'true' },
    { name: 'zone', kind: 'timezone', group: 'advanced', required: false, default: 'UTC' },
    { name: 'retries', kind: 'number', group: 'advanced', required: false, default: '3' },
    { name: 'token', kind: 'secret', group: 'advanced', required: false },
  ] as ConnectorField[],
}

/** A form holding the sample fields, which shows its values as they change. */
function Harness({ errors = {} }: { errors?: Record<string, { type: string; message: string }> }) {
  const { control, register } = useForm<SchemaValues>({ defaultValues: { config: {}, secrets: {} } })
  const config = useWatch({ control, name: 'config' })
  return (
    <>
      <SchemaFields
        connector={sample}
        groups={['connection', 'advanced', 'jump']}
        control={control}
        register={register}
        errors={{ config: errors } as never}
        saved={['token']}
        exclude={{ mode: ['safe'] }}
        idPrefix="sample"
      />
      <output aria-label="values">{JSON.stringify(config)}</output>
    </>
  )
}

/**
 * Covers {@link SchemaFields}: the fields of any connector drawn from its schema (ADR-0022).
 *
 * Scenarios:
 * - a field without translations is labelled by its name, a host shows its default and without a port stands alone, and the groups without fields are left out
 * - an optional choice offers an empty choice and leaves out the excluded ones, a required one without choices offers none; a boolean is a checkbox sending true or false, warning about a risky value
 * - a time zone suggests the zones the browser knows, a number shows its default, a stored secret says it is kept
 * - a validation error is worded by its reason under its field
 */
describe('SchemaFields', () => {
  it('draws every kind of field of a connector without translations', async () => {
    const user = userEvent.setup()
    await renderWithProviders(<Harness />)

    expect(screen.getByLabelText('endpoint')).toHaveAttribute('placeholder', 'storage.example.com')
    expect(screen.queryByText('Port')).not.toBeInTheDocument()
    expect(screen.queryAllByRole('heading')).toHaveLength(0)
    expect(await optionsOf(user, screen.getByLabelText('flavor'))).toEqual([])
    await user.keyboard('{Escape}')
    expect(await optionsOf(user, screen.getByLabelText('mode'))).toEqual(['', 'fast'])
    await user.keyboard('{Escape}')
    await choose(user, screen.getByLabelText('mode'), 'fast')

    await user.click(screen.getByRole('checkbox', { name: 'secure' }))
    expect(screen.getByRole('checkbox', { name: 'secure' })).toBeChecked()
    expect(screen.getByLabelText('values')).toHaveTextContent('"secure":"true"')
    await user.click(screen.getByRole('checkbox', { name: 'secure' }))
    expect(screen.getByLabelText('values')).toHaveTextContent('"secure":"false"')
    expect(screen.getByLabelText('values')).toHaveTextContent('"mode":"fast"')

    expect(screen.getByRole('checkbox', { name: 'Use HTTPS' })).toBeChecked()
    expect(screen.queryByRole('note')).not.toBeInTheDocument()
    await user.click(screen.getByRole('checkbox', { name: 'Use HTTPS' }))
    expect(screen.getByRole('note')).toHaveTextContent('travel in clear text')

    expect(screen.getByLabelText('zone')).toHaveAttribute('list', 'sample-zone-zones')
    expect(screen.getByLabelText('zone')).toHaveAttribute('placeholder', 'UTC')
    expect(screen.getByLabelText('retries')).toHaveAttribute('inputmode', 'numeric')
    expect(screen.getByLabelText('retries')).toHaveAttribute('placeholder', '3')
    expect(screen.getByLabelText('token')).toHaveAttribute('placeholder', 'Leave blank to keep the stored one.')
  })

  it('words a validation error by its reason', async () => {
    await renderWithProviders(<Harness errors={{ endpoint: { type: 'schema', message: 'host' }, retries: { type: 'schema', message: 'gibberish' } }} />)

    expect(screen.getByText('Enter a host name or address, without scheme, port or path.')).toBeInTheDocument()
    expect(screen.getByText('Choose one of the options.')).toBeInTheDocument()
    expect(screen.getByLabelText('endpoint')).toHaveAttribute('aria-invalid', 'true')
  })
})
