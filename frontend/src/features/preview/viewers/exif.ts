import exifr from 'exifr/dist/lite.esm.mjs'

/** The metadata of a photo the viewer shows, by field, as text. */
export type PhotoInfo = Partial<Record<'camera' | 'taken' | 'exposure' | 'aperture' | 'iso' | 'focal' | 'software' | 'location', string>>

/** What exifr answers, for the tags the viewer shows. */
interface Tags {
  /** Maker of the camera. */
  Make?: string
  /** Model of the camera. */
  Model?: string
  /** When the photo was taken. */
  DateTimeOriginal?: Date
  /** Exposure time, in seconds. */
  ExposureTime?: number
  /** Aperture, as the f-number. */
  FNumber?: number
  /** Sensitivity. */
  ISO?: number
  /** Focal length, in millimeters. */
  FocalLength?: number
  /** Program that wrote the file. */
  Software?: string
  /** Latitude, in degrees. */
  latitude?: number
  /** Longitude, in degrees. */
  longitude?: number
}

/**
 * Reads the camera metadata of a photo (EXIF, with GPS turned into degrees) with the lite bundle of exifr, keeping only what the viewer
 * shows. A file without metadata, or whose metadata cannot be read, gives an empty object.
 */
export async function readPhotoInfo(bytes: Uint8Array): Promise<PhotoInfo> {
  let found: Tags | undefined
  try {
    found = (await exifr.parse(bytes, { gps: true })) as Tags | undefined
  } catch {
    return {}
  }
  if (found === undefined) {
    return {}
  }
  const camera = [found.Make, found.Model].filter((part) => part !== undefined).join(' ')
  const entries: Array<[keyof PhotoInfo, string | undefined]> = [
    ['camera', camera === '' ? undefined : camera],
    ['taken', found.DateTimeOriginal?.toISOString()],
    ['exposure', found.ExposureTime === undefined ? undefined : found.ExposureTime < 1 ? `1/${Math.round(1 / found.ExposureTime)} s` : `${found.ExposureTime} s`],
    ['aperture', found.FNumber === undefined ? undefined : `f/${found.FNumber}`],
    ['iso', found.ISO === undefined ? undefined : String(found.ISO)],
    ['focal', found.FocalLength === undefined ? undefined : `${found.FocalLength} mm`],
    ['software', found.Software],
    ['location', found.latitude === undefined || found.longitude === undefined ? undefined : `${found.latitude.toFixed(5)}, ${found.longitude.toFixed(5)}`],
  ]
  return Object.fromEntries(entries.filter(([, value]) => value !== undefined))
}
