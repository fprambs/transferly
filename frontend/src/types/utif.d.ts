/** The part of UTIF.js the image viewer uses (https://github.com/photopea/UTIF.js); the package ships no types. */
declare module 'utif' {
  /** An image file directory of a TIFF file: its tags, and its pixels once decoded. */
  export interface IFD {
    /** Width in pixels, once decoded. */
    width: number
    /** Height in pixels, once decoded. */
    height: number
    /** Tags of the directory, such as t256 for the width. */
    [tag: string]: unknown
  }
  /** Reads the directories of a TIFF file. */
  export function decode(buffer: ArrayBuffer): IFD[]
  /** Decodes the pixels of a directory in place. */
  export function decodeImage(buffer: ArrayBuffer, ifd: IFD): void
  /** Returns the pixels of a decoded directory as RGBA bytes. */
  export function toRGBA8(ifd: IFD): Uint8Array
}
