/** The calendar date of a day written as YYYY-MM-DD, at midnight of the browser, or `undefined` for an empty text. */
export function dateOfDay(day: string): Date | undefined {
  if (day === '') {
    return undefined
  }
  const [year, month, date] = day.split('-').map(Number)
  return new Date(year, month - 1, date)
}

/** A calendar date written as YYYY-MM-DD, read in the browser as the calendar shows it. */
export function dayOfDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

/** A day written as YYYY-MM-DD in words of a language, such as "Sep 29, 2026". */
export function formatDay(day: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(dateOfDay(day))
}
