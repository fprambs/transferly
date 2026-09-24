const physicalUtilities = [
  /(?:^|[\s"'`:])-?(?:ml|mr|pl|pr|left|right|scroll-ml|scroll-mr|scroll-pl|scroll-pr)-[\w[\]./%-]+/,
  /(?:^|[\s"'`:])text-(?:left|right)(?=$|[\s"'`])/,
  /(?:^|[\s"'`:])float-(?:left|right)(?=$|[\s"'`])/,
  /(?:^|[\s"'`:])(?:rounded|border)-(?:l|r|tl|tr|bl|br)(?:-[\w[\]./]+)?(?=$|[\s"'`])/,
]

/** A physical direction utility found in a class string. */
export interface DirectionViolation {
  /** One-based line of the offending class. */
  line: number
  /** The offending utility. */
  utility: string
}

/**
 * Finds Tailwind utilities tied to the physical left or right side, such as `ml-2` or `text-left`.
 * Logical utilities (`ms-2`, `text-start`) flip automatically for right-to-left languages.
 *
 * @param text - Source text of a component.
 */
export function findPhysicalDirectionUtilities(text: string): DirectionViolation[] {
  return text.split('\n').flatMap((line, index) =>
    line.includes('className') || line.includes('cva(') || line.trimStart().startsWith('"')
      ? physicalUtilities.flatMap((pattern) => {
          const match = pattern.exec(line)
          return match ? [{ line: index + 1, utility: match[0].trim().replace(/^["'`:]/, '') }] : []
        })
      : [],
  )
}
