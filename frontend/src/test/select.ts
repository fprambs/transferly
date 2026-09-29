import { waitFor, within } from '@testing-library/react'
import type { UserEvent } from '@testing-library/user-event'

/**
 * Opens the list of a {@link Select} and returns it, leaving out the list of a select still closing; looking inside it only keeps the
 * queries fast in large pages.
 */
async function openList(user: UserEvent, trigger: HTMLElement): Promise<HTMLElement> {
  await user.click(trigger)
  return waitFor(() => {
    const list = Array.from(document.querySelectorAll<HTMLElement>('[role="listbox"]')).findLast((candidate) => candidate.closest('[data-closed]') === null)
    if (list === undefined) {
      throw new Error('the list of options did not open')
    }
    return list
  })
}

/** Chooses an option of a {@link Select}: opens its list from the trigger and clicks the option with the name given. */
export async function choose(user: UserEvent, trigger: HTMLElement, option: string | RegExp): Promise<void> {
  const list = await openList(user, trigger)
  await user.click(within(list).getByRole('option', { name: option }))
}

/** Opens the list of a {@link Select}, reads the names of its options and closes it again with Escape. */
export async function optionsOf(user: UserEvent, trigger: HTMLElement): Promise<string[]> {
  const list = await openList(user, trigger)
  const names = Array.from(list.querySelectorAll('[role="option"]'), (option) => (option.textContent ?? '').trim())
  await user.keyboard('{Escape}')
  return names
}
