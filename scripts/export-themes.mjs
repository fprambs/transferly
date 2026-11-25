// Writes the themes of the web interface (frontend/src/lib/theme/themes.ts) to tui/themes.json, so the terminal client wears exactly
// the same colors. Run by scripts/generate.sh (make gen) with Node, which strips the types of the TypeScript source.
import { writeFileSync } from 'node:fs'

import { defaultThemeId, systemLightThemeId, themes } from '../frontend/src/lib/theme/themes.ts'

const out = new URL('../tui/themes.json', import.meta.url)
writeFileSync(out, `${JSON.stringify({ defaultThemeId, systemLightThemeId, themes }, null, 2)}\n`)
