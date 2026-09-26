/** Builds the link that opens an invitation: the token sits in the fragment, so it never reaches a server log or a `Referer` header. */
export function invitationLink(origin: string, token: string): string {
  return `${origin}/invite#${token}`
}
