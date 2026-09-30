import YAML from 'yaml'

/** One node of a structured file drawn as a tree: a value of JSON or YAML, or an element or text of XML. */
export interface TreeNode {
  /** The key or index of the value in its parent, or the name of an element; empty at the root. */
  label: string
  /** What the node is, which picks how its value is drawn. */
  type: 'object' | 'array' | 'string' | 'number' | 'boolean' | 'null' | 'element' | 'text'
  /** The value of a leaf, as text. */
  value?: string
  /** The attributes of an element, in order. */
  attributes?: Array<[string, string]>
  /** The children of an object, array or element. */
  children?: TreeNode[]
}

/** Turns a parsed JSON or YAML value into its tree. */
export function treeOfValue(value: unknown, label = ''): TreeNode {
  if (Array.isArray(value)) {
    return { label, type: 'array', children: value.map((item, index) => treeOfValue(item, String(index))) }
  }
  if (value instanceof Date) {
    return { label, type: 'string', value: value.toISOString() }
  }
  if (value !== null && typeof value === 'object') {
    return { label, type: 'object', children: Object.entries(value).map(([key, item]) => treeOfValue(item, key)) }
  }
  if (typeof value === 'string') {
    return { label, type: 'string', value }
  }
  if (typeof value === 'number' || typeof value === 'bigint') {
    return { label, type: 'number', value: String(value) }
  }
  if (typeof value === 'boolean') {
    return { label, type: 'boolean', value: String(value) }
  }
  return { label, type: 'null', value: 'null' }
}

/** Turns an XML element into its tree: its attributes, child elements and the text between them. */
function treeOfElement(element: Element): TreeNode {
  const children: TreeNode[] = []
  for (const child of element.childNodes) {
    if (child.nodeType === child.ELEMENT_NODE) {
      children.push(treeOfElement(child as Element))
    } else if ((child.nodeType === child.TEXT_NODE || child.nodeType === child.CDATA_SECTION_NODE) && (child as Text).data.trim() !== '') {
      children.push({ label: '', type: 'text', value: (child as Text).data.trim() })
    }
  }
  return { label: element.tagName, type: 'element', attributes: [...element.attributes].map((attribute) => [attribute.name, attribute.value]), children }
}

/** The error of a file that did not parse: its message, as the parser wrote it. */
export class ParseError extends Error {}

/** Whether a name is of a JSON Lines file, one value per line. */
function isJsonLines(name: string): boolean {
  return /\.(jsonl|ndjson)$/i.test(name)
}

/**
 * Parses the text of a structured file into its tree: JSON (one value, or one per line for JSON Lines), YAML (with the default limit of
 * aliases, which stops exponential expansion) or XML (read by the browser, which neither loads external entities nor runs scripts). A text
 * that does not parse throws a {@link ParseError} with the message of its parser.
 */
export function parseTree(kind: 'json' | 'yaml' | 'xml', name: string, text: string): TreeNode {
  try {
    if (kind === 'json') {
      return treeOfValue(isJsonLines(name) ? text.split('\n').filter((line) => line.trim() !== '').map((line) => JSON.parse(line) as unknown) : (JSON.parse(text) as unknown))
    }
    if (kind === 'yaml') {
      return treeOfValue(YAML.parse(text))
    }
  } catch (error) {
    throw new ParseError((error as Error).message)
  }
  const document = new DOMParser().parseFromString(text, 'application/xml')
  const failure = document.getElementsByTagName('parsererror')[0] as Element | undefined
  if (failure !== undefined) {
    throw new ParseError(failure.textContent as string)
  }
  return treeOfElement(document.documentElement)
}
