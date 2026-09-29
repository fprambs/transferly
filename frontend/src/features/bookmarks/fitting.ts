/** Width of the button that holds the bookmarks that do not fit, in pixels. */
const moreWidth = 28

/** Space between chips, in pixels, as the bar's gap draws it. */
const gap = 4

/**
 * How many chips fit in a bar, in order: all of them when they do, otherwise as many as leave room for the button that holds the rest.
 *
 * @param widths - Width of each chip, in pixels.
 * @param available - Width of the bar, in pixels.
 */
export function fitting(widths: readonly number[], available: number): number {
  const ends: number[] = []
  let end = -gap
  for (const width of widths) {
    end += gap + width
    ends.push(end)
  }
  if (end <= available) {
    return widths.length
  }
  return ends.filter((edge) => edge + gap + moreWidth <= available).length
}
