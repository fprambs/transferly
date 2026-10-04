import { useId, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

/** Draws the inside of an icon; `id` prefixes the ids of its gradients, so several icons on one page do not share them. */
type Artwork = (id: string) => ReactNode

/** The tile of the protocols without a brand, SFTP and FTP: a rounded square with a diagonal gradient between two colors. */
function tile(id: string, from: string, to: string): ReactNode {
  return (
    <>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor={from} />
          <stop offset="1" stopColor={to} />
        </linearGradient>
      </defs>
      <rect x="3" y="3" width="42" height="42" rx="11" fill={`url(#${id})`} />
    </>
  )
}

/** The view box and the artwork of each icon a connector descriptor names. */
const icons: Readonly<Record<string, { viewBox: string; art: Artwork }>> = {
  sftp: {
    viewBox: '0 0 48 48',
    art: (id) => (
      <>
        {tile(`${id}-sftp`, '#34D399', '#047857')}
        <path d="M12 15.5l6.5 5.5-6.5 5.5" fill="none" stroke="#FFFFFF" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M21.5 27.5h5" fill="none" stroke="#FFFFFF" strokeWidth="3.2" strokeLinecap="round" />
        <rect x="27" y="27" width="13" height="12" rx="3" fill="#064E3B" />
        <path d="M30.5 31v-2a3 3 0 0 1 6 0v2" fill="none" stroke="#A7F3D0" strokeWidth="2" strokeLinecap="round" />
        <rect x="29.5" y="31" width="8" height="6" rx="1.5" fill="#A7F3D0" />
      </>
    ),
  },
  ftps: {
    viewBox: '0 0 48 48',
    art: (id) => (
      <>
        {tile(`${id}-ftp`, '#818CF8', '#3730A3')}
        <path d="M18 34V14M12 20l6-6 6 6" fill="none" stroke="#FFFFFF" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M30 14v20M24 28l6 6 6-6" fill="none" stroke="#C7D2FE" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round" />
      </>
    ),
  },
  webdav: {
    viewBox: '0 0 48 48',
    art: (id) => (
      <>
        {tile(`${id}-webdav`, '#FB923C', '#C2410C')}
        <circle cx="20.5" cy="20.5" r="9.5" fill="none" stroke="#FFFFFF" strokeWidth="2.8" />
        <ellipse cx="20.5" cy="20.5" rx="4" ry="9.5" fill="none" stroke="#FFFFFF" strokeWidth="2.4" />
        <path d="M11 20.5h19" fill="none" stroke="#FFFFFF" strokeWidth="2.4" />
        <rect x="26" y="26" width="16" height="14" rx="3" fill="#7C2D12" />
        <path d="M28.5 30.5a1.5 1.5 0 0 1 1.5-1.5h3l1.5 1.5h4a1.5 1.5 0 0 1 1.5 1.5v4.5a1.5 1.5 0 0 1-1.5 1.5H30a1.5 1.5 0 0 1-1.5-1.5z" fill="#FED7AA" />
      </>
    ),
  },
  cloud: {
    viewBox: '0 0 64 64',
    art: () => (
      <>
        <rect width="64" height="64" fill="#7AA116" />
        <path
          transform="translate(11 10)"
          fill="#FFFFFF"
          fillRule="evenodd"
          d="M36.759,23.948 L36.993,22.131 C39.166,23.385 39.842,24.121 40.037,24.424 C39.713,24.485 38.832,24.509 36.759,23.948 Z M19.5,11 C9.125,11 2.382,8.488 2,6.696 L2,6.511 C2.349,5.392 8.14,2 19.5,2 C30.84,2 36.632,5.381 37,6.505 L37,6.616 C36.724,8.446 29.961,11 19.5,11 Z M34.818,23.362 C30.053,21.797 23.912,18.979 21.594,17.888 C21.535,16.786 20.628,15.906 19.512,15.906 C18.358,15.906 17.418,16.845 17.418,18 C17.418,19.154 18.358,20.094 19.512,20.094 C19.969,20.094 20.389,19.942 20.733,19.693 C23.132,20.822 29.619,23.798 34.558,25.38 L32.837,38.724 C32.583,40.352 27.919,42 19.497,42 C11.072,42 6.405,40.352 6.155,38.751 L2.435,9.814 C5.945,11.915 12.749,13 19.5,13 C26.25,13 33.054,11.915 36.565,9.815 Z M39,6.5 C39,3.431 30.661,0 19.5,0 C8.339,0 0,3.431 0,6.5 L0,7.043 L4.174,39.033 C4.875,43.516 15.123,44 19.497,44 C23.869,44 34.113,43.516 34.818,39.007 L36.501,25.947 C37.778,26.279 38.812,26.456 39.616,26.455 C40.606,26.455 41.273,26.203 41.697,25.697 C42.047,25.281 42.185,24.756 42.085,24.221 C41.857,23 40.403,21.712 37.37,20.044 L37.27,19.989 L39,7.044 Z"
        />
      </>
    ),
  },
  gcs: {
    viewBox: '0 0 512 512',
    art: () => (
      <>
        <path
          fill="#34A853"
          d="M442,277.9H70c-8.8,0-16,7.2-16,16v148.1c0,8.8,7.2,16,16,16h107.2c1.5.5,3.1.7,4.8.7s3.3-.3,4.8-.7h255.2c8.8,0,16-7.2,16-16v-148.1c0-8.8-7.2-16-16-16ZM86,309.9h80v116.1h-80v-116.1ZM426,425.9h-228v-116.1h228v116.1Z"
        />
        <path
          fill="#FBBC04"
          d="M442,54H70c-8.8,0-16,7.2-16,16v148.8c0,8.8,7.2,16,16,16h372c8.8,0,16-7.2,16-16V70c0-8.8-7.2-16-16-16ZM86,86h80v116.8h-80v-116.8ZM426,202.8h-228v-116.8h228v116.8Z"
        />
        <path fill="#EA4335" d="M442,234.8h-16V86H54v-16c0-8.8,7.2-16,16-16h372c8.8,0,16,7.2,16,16v148.8c0,8.8-7.2,16-16,16Z" />
        <path fill="#4285F4" d="M442,457.9h-16v-148.1H54v-16c0-8.8,7.2-16,16-16h372c8.8,0,16,7.2,16,16v148.1c0,8.8-7.2,16-16,16Z" />
        <circle fill="#EA4335" cx="349" cy="144.4" r="37" />
        <circle fill="#4285F4" cx="349" cy="367.9" r="37" />
      </>
    ),
  },
  gdrive: {
    viewBox: '0 -4.65 87.3 87.3',
    art: () => (
      <>
        <path d="m6.6 66.85 3.85 6.65c.8 1.4 1.95 2.5 3.3 3.3l13.75-23.8h-27.5c0 1.55.4 3.1 1.2 4.5z" fill="#0066DA" />
        <path d="m43.65 25-13.75-23.8c-1.35.8-2.5 1.9-3.3 3.3l-25.4 44a9.06 9.06 0 0 0 -1.2 4.5h27.5z" fill="#00AC47" />
        <path d="m73.55 76.8c1.35-.8 2.5-1.9 3.3-3.3l1.6-2.75 7.65-13.25c.8-1.4 1.2-2.95 1.2-4.5h-27.502l5.852 11.5z" fill="#EA4335" />
        <path d="m43.65 25 13.75-23.8c-1.35-.8-2.9-1.2-4.5-1.2h-18.5c-1.6 0-3.15.45-4.5 1.2z" fill="#00832D" />
        <path d="m59.8 53h-32.3l-13.75 23.8c1.35.8 2.9 1.2 4.5 1.2h50.8c1.6 0 3.15-.45 4.5-1.2z" fill="#2684FC" />
        <path d="m73.4 26.5-12.7-22c-.8-1.4-1.95-2.5-3.3-3.3l-13.75 23.8 16.15 28h27.45c0-1.55-.4-3.1-1.2-4.5z" fill="#FFBA00" />
      </>
    ),
  },
  azblob: {
    viewBox: '0 0 18 18',
    art: (id) => (
      <>
        <defs>
          <linearGradient id={`${id}-azblob`} x1="9" y1="15.834" x2="9" y2="5.788" gradientUnits="userSpaceOnUse">
            <stop offset="0" stopColor="#32BEDD" />
            <stop offset="0.775" stopColor="#32D4F5" />
          </linearGradient>
        </defs>
        <path d="M.5,5.788h17v9.478a.568.568,0,0,1-.568.568H1.068A.568.568,0,0,1,.5,15.266Z" fill={`url(#${id}-azblob)`} />
        <path d="M1.071,2.166H16.929a.568.568,0,0,1,.568.568V5.788H.5V2.734A.568.568,0,0,1,1.071,2.166Z" fill="#0078D4" />
        <rect x="2.328" y="7.049" width="6.281" height="3.408" rx="0.283" fill="#0078D4" />
        <rect x="9.336" y="7.049" width="6.281" height="3.408" rx="0.283" fill="#FFFFFF" />
        <rect x="2.296" y="11.128" width="6.281" height="3.408" rx="0.283" fill="#0078D4" />
        <rect x="9.304" y="11.128" width="6.281" height="3.408" rx="0.283" fill="#0078D4" />
      </>
    ),
  },
  azfiles: {
    viewBox: '0 0 18 18',
    art: (id) => (
      <>
        <defs>
          <linearGradient id={`${id}-azfiles`} x1="9" y1="15.799" x2="9" y2="5.316" gradientUnits="userSpaceOnUse">
            <stop offset="0" stopColor="#32BEDD" />
            <stop offset="0.775" stopColor="#32D4F5" />
          </linearGradient>
        </defs>
        <path d="M.544,5.316H17.456v9.918a.565.565,0,0,1-.565.565H1.109a.565.565,0,0,1-.565-.565Z" fill={`url(#${id}-azfiles)`} />
        <path d="M1.112,2.2H16.888a.565.565,0,0,1,.565.565v2.55H.547V2.766A.565.565,0,0,1,1.112,2.2Z" fill="#0078D4" />
        <path
          d="M13.9,7.32H9.715a.224.224,0,0,1-.125-.038l-1.2-.795a.219.219,0,0,0-.124-.038H4.752a.224.224,0,0,0-.224.224v7.392a.223.223,0,0,0,.224.223H13.9a.224.224,0,0,0,.224-.223V7.544A.224.224,0,0,0,13.9,7.32Z"
          fill="#5EA0EF"
        />
        <rect x="5.154" y="6.905" width="2.318" height="0.464" rx="0.098" fill="#FFFFFF" />
        <rect x="5.154" y="6.905" width="0.464" height="0.464" rx="0.067" fill="#0078D4" />
        <path
          d="M13.9,7.309H9.3a.225.225,0,0,0-.159.066l-.739.739a.225.225,0,0,1-.159.066H4.752a.224.224,0,0,0-.224.224v5.65a.223.223,0,0,0,.224.223H13.9a.224.224,0,0,0,.224-.223V7.533A.224.224,0,0,0,13.9,7.309Z"
          fill="#FFFFFF"
          opacity="0.9"
        />
      </>
    ),
  },
}

/** The icon of a connector whose descriptor names an icon the interface does not know: a server with a plug, in the color of the text. */
const fallback = {
  viewBox: '0 0 24 24',
  art: () => (
    <g fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round">
      <path d="M5 3h11a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM3 15.5v-1A2.5 2.5 0 0 1 5.5 12H11M6.5 6.5h.01M6.5 15.5h.01" />
      <path d="M15 13v3M19 13v3M14 16h6v1.5a3 3 0 0 1-6 0zM17 20.5V22" className="text-primary" />
    </g>
  ),
}

/** Props of {@link ConnectorIcon}. */
export interface ConnectorIconProps {
  /** The icon the descriptor of the connector names, such as sftp, ftps or cloud. */
  icon: string
  /** Classes for the size and placement of the icon. */
  className?: string
}

/**
 * The icon of a connector, as the catalog, the store and the sites show it. Cloud services keep their official artwork and colors: the
 * Amazon S3 icon of the AWS architecture icons, the Cloud Storage icon of the Google Cloud product icons, the Google Drive logo and the
 * Blob and Files icons of the Azure service icons. SFTP, FTP and WebDAV, which have no brand, get tiles of their own. It is decorative.
 */
export function ConnectorIcon({ icon, className }: ConnectorIconProps) {
  const id = useId().replace(/[^a-zA-Z0-9_-]/g, '')
  const known = icons[icon]
  const { viewBox, art } = known ?? fallback
  return (
    <svg viewBox={viewBox} aria-hidden="true" data-icon={known === undefined ? 'connector' : icon} className={cn('size-5 shrink-0', className)}>
      {art(id)}
    </svg>
  )
}
