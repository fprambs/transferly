import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { useForm, useWatch } from 'react-hook-form'
import { describe, expect, it, vi } from 'vitest'
import type { SshKeyInfo } from '@/lib/api'
import { problem } from '@/test/handlers'
import { server } from '@/test/msw'
import { renderWithProviders } from '@/test/render'
import { KeyFileField, maxKeyFileBytes } from './KeyFileField'
import { siteDefaults, type SiteValues } from './site-form'

/** A form holding only the key field, which shows the key and passphrase it holds. */
function Harness({ saved = false }: { saved?: boolean }) {
  const { control } = useForm<SiteValues>({ defaultValues: siteDefaults(undefined, undefined, 'sftp') })
  const [key, passphrase] = useWatch({ control, name: ['secrets.private_key', 'secrets.passphrase'] })
  return (
    <>
      <KeyFileField control={control} name="secrets.private_key" passphraseName="secrets.passphrase" id="site-key" passphraseLabel="Key passphrase" saved={saved} />
      <output data-testid="values">{`${key ?? ""}|${passphrase ?? ""}`}</output>
    </>
  )
}

/** Answers inspections with what reply returns for the key and passphrase sent, and records the requests. */
function answer(reply: (body: { key: string; passphrase?: string }) => Response): Array<{ key: string; passphrase?: string }> {
  const seen: Array<{ key: string; passphrase?: string }> = []
  server.use(
    http.post('/api/v1/ssh-keys/inspect', async ({ request }) => {
      const body = (await request.json()) as { key: string; passphrase?: string }
      seen.push(body)
      return reply(body)
    }),
  )
  return seen
}

/** A key description with the given changes. */
function info(changes: Partial<SshKeyInfo>): Response {
  return HttpResponse.json({ private: true, format: 'ppk3', algorithm: 'ssh-rsa', bits: 2048, fingerprint: 'SHA256:print', encrypted: false, status: 'usable', ...changes })
}

/** Renders the field and returns a user. */
async function renderField(saved = false) {
  const user = userEvent.setup()
  await renderWithProviders(<Harness saved={saved} />)
  return user
}

/**
 * Covers {@link KeyFileField}: a private key given as a file and described by the server (4.8g).
 *
 * Scenarios:
 * - a chosen file is read in the browser, kept in the form and described: format, kind, type, size and fingerprint, and ready to use
 * - an encrypted key asks for its passphrase, checks it again as it is typed, and says when it does not open the key
 * - a public key is told apart, and what the file does not tell before the passphrase is left out
 * - a file that is no key, an unsupported key with its reason, a file too large to be a key and a failed check are explained
 * - a file can be dropped, removed, or the choice cancelled; the button opens the file picker; a saved key is said to be kept
 */
describe('KeyFileField', () => {
  it('reads a chosen file and describes the key', async () => {
    const seen = answer(() => info({}))
    const user = await renderField()

    await user.upload(screen.getByLabelText('Key file'), new File(['PRIVATE KEY TEXT'], 'deploy.ppk'))

    expect(await screen.findByText('Ready to use.')).toBeInTheDocument()
    expect(screen.getByText('deploy.ppk')).toBeInTheDocument()
    expect(screen.getByText('PuTTY PPK 3 · private key · ssh-rsa · 2048 bits')).toBeInTheDocument()
    expect(screen.getByText('SHA256:print')).toBeInTheDocument()
    expect(screen.queryByLabelText('Key passphrase')).not.toBeInTheDocument()
    expect(screen.getByTestId('values')).toHaveTextContent('PRIVATE KEY TEXT|')
    expect(seen).toEqual([{ key: 'PRIVATE KEY TEXT' }])
  })

  it('asks for the passphrase of an encrypted key and checks it as it is typed', async () => {
    const seen = answer(({ passphrase }) =>
      info({ encrypted: true, status: passphrase === undefined ? 'passphrase_required' : passphrase === 'phrase' ? 'usable' : 'wrong_passphrase' }),
    )
    const user = await renderField()
    await user.upload(screen.getByLabelText('Key file'), new File(['KEY'], 'id_rsa'))

    expect(await screen.findByText('Enter the passphrase of this key.')).toBeInTheDocument()
    expect(screen.getByText(/protected by a passphrase/)).toBeInTheDocument()
    await user.type(screen.getByLabelText('Key passphrase'), 'nope')
    expect(await screen.findByText('The passphrase does not open this key.')).toBeInTheDocument()
    await user.clear(screen.getByLabelText('Key passphrase'))
    await user.type(screen.getByLabelText('Key passphrase'), 'phrase')
    expect(await screen.findByText('Ready to use.')).toBeInTheDocument()
    expect(seen.at(-1)).toEqual({ key: 'KEY', passphrase: 'phrase' })
  })

  it('tells a public key apart and leaves out what the file does not tell', async () => {
    let reply = info({ private: false, format: 'openssh', status: 'public_key' })
    answer(() => reply)
    const user = await renderField()
    await user.upload(screen.getByLabelText('Key file'), new File(['ssh-rsa AAAA'], 'id_rsa.pub'))
    expect(await screen.findByText(/This is a public key/)).toBeInTheDocument()
    expect(screen.getByText('OpenSSH · public key · ssh-rsa · 2048 bits')).toBeInTheDocument()

    reply = HttpResponse.json({ private: true, format: 'pem', encrypted: true, status: 'passphrase_required' })
    await user.click(screen.getByRole('button', { name: 'Remove the key file' }))
    await user.upload(screen.getByLabelText('Key file'), new File(['PEM'], 'legacy.pem'))
    expect(await screen.findByText('PEM · private key · protected by a passphrase')).toBeInTheDocument()
    expect(screen.queryByText(/SHA256/)).not.toBeInTheDocument()

    reply = info({ private: false, format: 'openssh', algorithm: 'ssh-ed25519-cert-v01@openssh.com', bits: undefined, status: 'public_key' })
    await user.click(screen.getByRole('button', { name: 'Remove the key file' }))
    await user.upload(screen.getByLabelText('Key file'), new File(['CERT'], 'id-cert.pub'))
    expect(await screen.findByText('OpenSSH · public key · ssh-ed25519-cert-v01@openssh.com')).toBeInTheDocument()
  })

  it('explains keys that cannot be used and checks that fail', async () => {
    let reply: Response = problem(422, 'private_key_unreadable')
    answer(() => reply)
    const user = await renderField()
    const choose = async (name: string): Promise<void> => {
      await user.click(screen.getByRole('button', { name: 'Remove the key file' }))
      await user.upload(screen.getByLabelText('Key file'), new File(['X'], name))
    }
    await user.upload(screen.getByLabelText('Key file'), new File(['X'], 'notes.txt'))
    expect(await screen.findByText('This file is not a key Transferly can read.')).toBeInTheDocument()

    reply = problem(422, 'private_key_unsupported', { reason: 'argon2d' })
    await choose('argon2d.ppk')
    expect(await screen.findByText('This PuTTY key uses Argon2d. Save it again in PuTTYgen with the default, Argon2id.')).toBeInTheDocument()

    reply = problem(401, 'unauthenticated')
    await choose('signed-out.ppk')
    expect(await screen.findByText('The key could not be checked. Try again.')).toBeInTheDocument()

    reply = HttpResponse.error()
    await choose('offline.ppk')
    await waitFor(() => expect(screen.getByText('offline.ppk')).toBeInTheDocument())
    expect(await screen.findByText('The key could not be checked. Try again.')).toBeInTheDocument()
  })

  it('refuses a file too large to be a key without sending it', async () => {
    const seen = answer(() => info({}))
    const user = await renderField()
    await user.upload(screen.getByLabelText('Key file'), new File(['x'.repeat(maxKeyFileBytes + 1)], 'movie.mp4'))

    expect(await screen.findByText('This file is too large to be a key.')).toBeInTheDocument()
    expect(screen.getByTestId('values')).toHaveTextContent('|')
    expect(seen).toEqual([])
  })

  it('takes a dropped file, can remove it, and ignores a cancelled choice', async () => {
    answer(() => info({}))
    const user = await renderField(true)
    expect(screen.getByText('A key is saved. Choose or drop another file to replace it.')).toBeInTheDocument()
    const click = vi.spyOn(screen.getByLabelText<HTMLInputElement>('Key file'), 'click')
    await user.click(screen.getByRole('button', { name: 'Choose a key file' }))
    expect(click).toHaveBeenCalled()

    await user.upload(screen.getByLabelText('Key file'), [])
    expect(screen.getByRole('button', { name: 'Choose a key file' })).toBeInTheDocument()

    const zone = screen.getByRole('button', { name: 'Choose a key file' }).parentElement as HTMLElement
    fireEvent.dragOver(zone)
    expect(zone).toHaveClass('border-accent')
    fireEvent.dragLeave(zone)
    expect(zone).not.toHaveClass('border-accent')
    fireEvent.drop(zone, { dataTransfer: { files: [] } })
    expect(screen.getByRole('button', { name: 'Choose a key file' })).toBeInTheDocument()
    fireEvent.drop(zone, { dataTransfer: { files: [new File(['DROPPED'], 'dropped.key')] } })
    expect(await screen.findByText('Ready to use.')).toBeInTheDocument()
    expect(screen.getByTestId('values')).toHaveTextContent('DROPPED|')

    await user.click(screen.getByRole('button', { name: 'Remove the key file' }))
    expect(screen.getByRole('button', { name: 'Choose a key file' })).toBeInTheDocument()
    expect(screen.getByTestId('values')).toHaveTextContent('|')
  })
})
