import { Suspense } from 'react'
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { PreviewKind } from '../detect'
import { textSource } from '@/test/preview'
import { viewerOf } from './registry'

vi.mock('./ArchiveViewer', () => ({ ArchiveViewer: () => <p>ArchiveViewer</p> }))
vi.mock('./CertificateViewer', () => ({ CertificateViewer: () => <p>CertificateViewer</p> }))
vi.mock('./CsvViewer', () => ({ CsvViewer: () => <p>CsvViewer</p> }))
vi.mock('./EmailViewer', () => ({ EmailViewer: () => <p>EmailViewer</p> }))
vi.mock('./ImageViewer', () => ({ ImageViewer: () => <p>ImageViewer</p> }))
vi.mock('./MarkdownViewer', () => ({ MarkdownViewer: () => <p>MarkdownViewer</p> }))
vi.mock('./MediaViewer', () => ({ MediaViewer: () => <p>MediaViewer</p> }))
vi.mock('./IsolatedViewer', () => ({ IsolatedViewer: () => <p>IsolatedViewer</p> }))
vi.mock('./PdfViewer', () => ({ PdfViewer: () => <p>PdfViewer</p> }))
vi.mock('./StructuredViewer', () => ({ StructuredViewer: () => <p>StructuredViewer</p> }))
vi.mock('./TextViewer', () => ({ TextViewer: () => <p>TextViewer</p> }))

/** Checks that each deferred viewer resolves its module export through a Suspense boundary. */
describe('deferred preview viewers', () => {
  it('opens every deferred kind and leaves unsupported kinds without a viewer', async () => {
    const kinds: PreviewKind[] = ['archive', 'certificate', 'csv', 'email', 'image', 'markdown', 'audio', 'document', 'pdf', 'json', 'text']
    render(<Suspense fallback={<p>Loading viewers</p>}>{kinds.map((kind) => {
      const Viewer = viewerOf(kind)!
      return <Viewer key={kind} kind={kind} source={textSource('sample.txt', 'sample')} />
    })}</Suspense>)
    expect(await screen.findByText('ArchiveViewer')).toBeInTheDocument()
    expect(await screen.findByText('CertificateViewer')).toBeInTheDocument()
    expect(await screen.findByText('CsvViewer')).toBeInTheDocument()
    expect(await screen.findByText('EmailViewer')).toBeInTheDocument()
    expect(await screen.findByText('ImageViewer')).toBeInTheDocument()
    expect(await screen.findByText('MarkdownViewer')).toBeInTheDocument()
    expect(await screen.findByText('MediaViewer')).toBeInTheDocument()
    expect(await screen.findByText('IsolatedViewer')).toBeInTheDocument()
    expect(await screen.findByText('PdfViewer')).toBeInTheDocument()
    expect(await screen.findByText('StructuredViewer')).toBeInTheDocument()
    expect(await screen.findByText('TextViewer')).toBeInTheDocument()
    expect(viewerOf('binary')).toBeUndefined()
  })
})
