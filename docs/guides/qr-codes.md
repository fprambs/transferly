# QR codes (qrcode)

Sources: the README of [`qrcode`](https://github.com/soldair/node-qrcode) 1.5, the Key URI format of authenticator apps and [ADR-0007](../adr/0007-second-factor.md). Used in `frontend/src/features/security/EnableTotpDialog.tsx`.

## Rules

- The address to encode (`otpauth://…`) contains the TOTP secret, so the code is drawn **in the browser** and the text is never sent to an external QR service. `QRCode.toDataURL(text, options)` returns a promise of a PNG `data:` URL, drawn without leaving the page.
- The image goes in an `<img>` (the CSP allows `img-src 'self' data:`), never as injected markup, and has a text alternative. The secret is also shown as text, so an app that cannot scan can be set up by typing it and a screen reader can read it.
- Error correction level `M` and a margin of one module give a code that scans reliably from a screen at 192 pixels; the white background is kept in dark themes because scanners need contrast.
- The query that generates the code lives only as long as the dialog (`gcTime: 0` for the setup), so the secret is not kept in the cache after closing.
