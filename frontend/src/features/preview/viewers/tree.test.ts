import { describe, expect, it } from 'vitest'

import { ParseError, parseTree, treeOfValue } from './tree'

/**
 * Covers {@link parseTree} and {@link treeOfValue} (ADR-0018).
 *
 * Scenarios:
 * - JSON values become trees of objects, arrays and typed leaves; JSON Lines is one value per non-empty line
 * - YAML with anchors parses, dates become ISO text, and an alias bomb is refused by the alias limit
 * - XML elements keep their attributes, child elements and text, CDATA included; blank text and comments are left out
 * - text that does not parse throws a ParseError with the message of its parser
 */
describe('structured trees', () => {
  it('reads JSON and JSON Lines', () => {
    const tree = parseTree('json', 'a.json', '{"name":"x","list":[1,true,null],"big":12}')
    expect(tree.type).toBe('object')
    expect(tree.children?.map((child) => [child.label, child.type])).toEqual([['name', 'string'], ['list', 'array'], ['big', 'number']])
    expect(tree.children?.[1].children?.map((child) => child.value)).toEqual(['1', 'true', 'null'])
    const lines = parseTree('json', 'events.JSONL', '{"a":1}\n\n{"a":2}\n')
    expect(lines.type).toBe('array')
    expect(lines.children).toHaveLength(2)
    expect(treeOfValue(10n)).toEqual({ label: '', type: 'number', value: '10' })
  })

  it('reads YAML and refuses an alias bomb', () => {
    const tree = parseTree('yaml', 'c.yaml', 'base: &b {port: 22}\ncopy: *b\nday: 2026-09-29')
    expect(tree.children?.[1].children?.[0]).toMatchObject({ label: 'port', value: '22' })
    expect(treeOfValue(new Date(Date.UTC(2026, 8, 29)))).toMatchObject({ type: 'string', value: '2026-09-29T00:00:00.000Z' })
    let bomb = 'a: &a [x,x,x,x,x,x,x,x,x]\n'
    let previous = 'a'
    for (const name of 'bcdefghij') {
      bomb += `${name}: &${name} [${Array(9).fill(`*${previous}`).join(',')}]\n`
      previous = name
    }
    expect(() => parseTree('yaml', 'bomb.yaml', bomb)).toThrow(ParseError)
  })

  it('reads XML', () => {
    const tree = parseTree('xml', 'f.xml', '<rss v="2"><item id="1">Hi<![CDATA[ there ]]>  <!-- note --></item>\n <empty/></rss>')
    expect(tree).toMatchObject({ label: 'rss', type: 'element', attributes: [['v', '2']] })
    expect(tree.children?.[0].children?.map((child) => child.value)).toEqual(['Hi', 'there'])
    expect(tree.children?.[1]).toMatchObject({ label: 'empty', children: [] })
  })

  it('reports what does not parse', () => {
    expect(() => parseTree('json', 'b.json', '{"a": }')).toThrow(ParseError)
    expect(() => parseTree('yaml', 'b.yaml', 'a: [unclosed')).toThrow(ParseError)
    expect(() => parseTree('xml', 'b.xml', '<a><b></a>')).toThrow(ParseError)
  })
})
