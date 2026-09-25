import { formatSize } from '../explorer/entries'

/** Formats a speed in bytes per second, such as `1.5 MB/s`. */
export function formatSpeed(bytesPerSecond: number): string {
  return `${formatSize(Math.round(bytesPerSecond))}/s`
}

/** Formats a duration in seconds as `45s`, `3m 05s` or `1h 02m`. */
export function formatDuration(seconds: number): string {
  if (seconds < 60) {
    return `${seconds}s`
  }
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) {
    return `${minutes}m ${String(seconds % 60).padStart(2, '0')}s`
  }
  return `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m`
}

/** Returns the completed share of a transfer as a whole percentage, or `undefined` when its size is unknown. */
export function percentDone(bytesDone: number, totalBytes: number | undefined): number | undefined {
  if (totalBytes === undefined || totalBytes <= 0) {
    return undefined
  }
  return Math.min(Math.round((bytesDone / totalBytes) * 100), 100)
}
