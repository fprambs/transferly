/**
 * Converts a six-digit hexadecimal color to its relative luminance as defined by WCAG 2.
 *
 * @param hex - Color such as `#0b0d11`.
 * @returns Luminance between 0 (black) and 1 (white).
 */
export function luminance(hex: string): number {
  const channels = [1, 3, 5].map((start) => Number.parseInt(hex.slice(start, start + 2), 16) / 255)
  const [red, green, blue] = channels.map((value) =>
    value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4,
  ) as [number, number, number]
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue
}

/**
 * Computes the WCAG 2 contrast ratio between two colors.
 *
 * @param first - One color, as a six-digit hexadecimal string.
 * @param second - The other color.
 * @returns Ratio from 1 (identical) to 21 (black on white).
 */
export function contrastRatio(first: string, second: string): number {
  const [lighter, darker] = [luminance(first), luminance(second)].sort((a, b) => b - a) as [number, number]
  return (lighter + 0.05) / (darker + 0.05)
}
