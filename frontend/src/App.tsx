import { AppShell } from '@/features/shell/AppShell'
import { useThemeSync } from '@/lib/theme/use-theme'

/** Application root: keeps the color scheme in sync and renders the layout. */
export default function App() {
  useThemeSync()
  return <AppShell />
}
