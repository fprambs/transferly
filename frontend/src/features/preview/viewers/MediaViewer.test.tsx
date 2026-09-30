import { act, cleanup, fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { bytesSource } from '@/test/preview'
import { renderWithProviders } from '@/test/render'

import type { MediaAddress, PreviewSource } from '../source'
import { MediaViewer } from './MediaViewer'

/** A source that plays from a fixed address and records when it is released. */
function playable(name: string, address = 'blob:media') {
  const release = vi.fn()
  const source: PreviewSource = { ...bytesSource(name, new Uint8Array([1, 2, 3])), mediaUrl: async () => ({ url: address, release }) }
  return { source, release }
}

/** Tells a media element that its metadata loaded. */
function loadMetadata(media: HTMLElement, duration: number, width?: number, height?: number): void {
  Object.defineProperty(media, 'duration', { value: duration, configurable: true })
  if (width !== undefined) {
    Object.defineProperty(media, 'videoWidth', { value: width })
    Object.defineProperty(media, 'videoHeight', { value: height })
  }
  fireEvent.loadedMetadata(media)
}

/**
 * Covers {@link MediaViewer} (ADR-0018).
 *
 * Scenarios:
 * - a video plays from the address of its source, and shows its length and picture size once its metadata loads
 * - the speed changes the playback rate, before and after the metadata loads
 * - audio plays in an audio player, long lengths show their hours, and a live stream without a length shows none
 * - the address is released when the viewer closes, even when it closes before the address arrives
 * - a format the browser cannot play says so and offers its bytes
 */
describe('MediaViewer', () => {
  it('plays a video at the speed asked for', async () => {
    const { source, release } = playable('clip.mp4')
    await renderWithProviders(<MediaViewer source={source} kind="video" />)
    const video = (await screen.findByLabelText('clip.mp4')) as HTMLVideoElement
    expect(video.tagName).toBe('VIDEO')
    expect(video).toHaveAttribute('src', 'blob:media')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: '1.5×' }))
    expect(video.playbackRate).toBe(1.5)
    loadMetadata(video, 125, 320, 180)
    expect(screen.getByText('2:05 · 320 × 180 px')).toBeInTheDocument()
    expect(video.playbackRate).toBe(1.5)
    cleanup()
    expect(release).toHaveBeenCalled()
  })

  it('plays audio and shows long lengths with hours', async () => {
    const { source } = playable('talk.mp3')
    await renderWithProviders(<MediaViewer source={source} kind="audio" />)
    const audio = await screen.findByLabelText('talk.mp3')
    expect(audio.tagName).toBe('AUDIO')
    loadMetadata(audio, 3700)
    expect(screen.getByText('1:01:40')).toBeInTheDocument()
    loadMetadata(audio, Number.POSITIVE_INFINITY)
    expect(screen.queryByText('1:01:40')).not.toBeInTheDocument()
  })

  it('releases an address that arrives after the viewer closed', async () => {
    const release = vi.fn()
    let resolve: (address: MediaAddress) => void = () => undefined
    const source: PreviewSource = { ...bytesSource('late.mp4', new Uint8Array()), mediaUrl: () => new Promise((done) => (resolve = done)) }
    await renderWithProviders(<MediaViewer source={source} kind="video" />)
    cleanup()
    await act(async () => resolve({ url: 'blob:late', release }))
    expect(release).toHaveBeenCalled()
  })

  it('offers the bytes of a format it cannot play', async () => {
    const { source } = playable('old.avi')
    await renderWithProviders(<MediaViewer source={source} kind="video" />)
    fireEvent.error(await screen.findByLabelText('old.avi'))
    expect(await screen.findByText('This browser cannot play this file')).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Show as bytes' }))
    expect(await screen.findByRole('table', { name: 'Bytes of old.avi' })).toBeInTheDocument()
  })
})
