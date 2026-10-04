import { ConnectorIcon } from '@/components/brand/ConnectorIcon'
import type { Connector } from '@/lib/api'

import { useConnectorTexts } from './use-connector-texts'

/** Props of {@link ConnectorCatalog}. */
export interface ConnectorCatalogProps {
  /** The connectors to offer, in the order they are drawn. */
  connectors: readonly Connector[]
  /** Called with the type of the connector chosen. */
  onChoose: (type: string) => void
}

/**
 * The connectors a new connection can use, as a catalog of tiles (ADR-0019): the icon of each service, its name, what it reaches and
 * whether it is in beta. Choosing one opens its form.
 */
export function ConnectorCatalog({ connectors, onChoose }: ConnectorCatalogProps) {
  const texts = useConnectorTexts()
  return (
    <ul aria-label={texts.catalog.title} className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3">
      {connectors.map((connector) => (
        <li key={connector.type} className="flex">
          <button
            type="button"
            onClick={() => onChoose(connector.type)}
            className="group relative flex w-full flex-col items-start gap-3 rounded-xl border bg-background p-4 text-start transition-colors outline-none hover:border-primary/60 hover:bg-primary/5 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <span aria-hidden="true" className="flex size-11 items-center justify-center rounded-xl bg-muted text-foreground transition-colors group-hover:bg-accent">
              <ConnectorIcon icon={connector.icon} className="size-7" />
            </span>
            <span className="flex flex-col gap-1">
              <span className="text-sm font-semibold">{connector.name}</span>
              <span className="text-xs leading-relaxed text-muted-foreground">{texts.description(connector)}</span>
            </span>
            {connector.maturity !== 'stable' && (
              <span className="absolute top-3 end-3 rounded-full bg-warn/15 px-2 py-0.5 text-[11px] font-medium text-warn-text">{texts.catalog.beta}</span>
            )}
          </button>
        </li>
      ))}
    </ul>
  )
}
