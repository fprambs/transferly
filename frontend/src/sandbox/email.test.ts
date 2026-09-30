import { describe, expect, it } from 'vitest'

import { renderEmail } from './email'

/**
 * Covers {@link renderEmail} (D2.7).
 *
 * Scenarios:
 * - the HTML of a message is drawn in a shadow root on a page of its own, keeping its styles, text, tables and pictures
 * - scripts, handlers, forms, frames, objects, meta and base elements are removed, and links do not navigate
 * - remote addresses of pictures, lists of them, backgrounds and CSS, inline or in style elements, are removed; local ones are kept
 */
describe('renderEmail', () => {
  it('draws the message cleaned inside a shadow root', () => {
    const root = document.createElement('div')
    document.body.append(root)
    renderEmail(
      '<html><head><style>p{color:red}</style><meta http-equiv="refresh" content="0;url=https://example.com"><base href="https://example.com/"><script>alert(1)</script></head>' +
        '<body onload="alert(2)"><p>Hola</p><table><tr><td>cell</td></tr></table><img src="data:image/png;base64,AAAA" onerror="alert(3)">' +
        '<a href="javascript:alert(4)">link</a><form action="https://example.com"><input name="p"></form><iframe src="https://example.com"></iframe><object data="x"></object></body></html>',
      root,
    )
    const shadow = (root.querySelector('.email') as HTMLElement).shadowRoot as ShadowRoot
    expect(shadow.querySelector('style')?.textContent).toBe('p{color:red}')
    expect(shadow.querySelector('p')?.textContent).toBe('Hola')
    expect(shadow.querySelector('td')?.textContent).toBe('cell')
    expect(shadow.querySelector('img')?.hasAttribute('onerror')).toBe(false)
    for (const selector of ['script', 'form', 'input', 'iframe', 'object', 'meta', 'base', '[onload]']) {
      expect(shadow.querySelector(selector)).toBeNull()
    }
    const link = shadow.querySelector('a') as HTMLAnchorElement
    expect(link.hasAttribute('href')).toBe(false)
    const click = new MouseEvent('click', { bubbles: true, cancelable: true, composed: true })
    link.dispatchEvent(click)
    expect(click.defaultPrevented).toBe(true)
    const outside = new MouseEvent('click', { bubbles: true, cancelable: true })
    ;(shadow.querySelector('p') as HTMLElement).dispatchEvent(outside)
    expect(outside.defaultPrevented).toBe(false)
  })

  it('removes what would load from the network', () => {
    const root = document.createElement('div')
    renderEmail(
      '<style>body{background:url("https://example.com/a.png")} p{color:blue}</style>' +
        '<img id="remote" src="https://example.com/pixel.gif"><img id="relative" src="//example.com/x.gif"><img id="set" srcset="a.png 1x, https://example.com/b.png 2x">' +
        '<img id="local" src="data:image/png;base64,AAAA"><table id="table" background="http://example.com/bg.png"><tr><td>x</td></tr></table>' +
        '<p id="styled" style="background-image: url(https://example.com/c.png); color: red">text</p><p id="plain">plain</p>',
      root,
    )
    const shadow = (root.querySelector('.email') as HTMLElement).shadowRoot as ShadowRoot
    expect(shadow.querySelector('style')?.textContent).toBe('body{background:none} p{color:blue}')
    for (const id of ['remote', 'relative', 'set']) {
      expect(shadow.querySelector(`#${id}`)?.hasAttribute(id === 'set' ? 'srcset' : 'src')).toBe(false)
    }
    expect(shadow.querySelector('#local')?.getAttribute('src')).toBe('data:image/png;base64,AAAA')
    expect(shadow.querySelector('#table')?.hasAttribute('background')).toBe(false)
    expect(shadow.querySelector('#styled')?.getAttribute('style')).toBe('background-image: none; color: red')
    expect(shadow.querySelector('#plain')?.hasAttribute('style')).toBe(false)
  })
})
