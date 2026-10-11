import type { Paragraph, Root, RootContent } from 'mdast'

/**
 * A remark plugin that turns a list made only of empty items back into the
 * text it was typed as.
 *
 * In Markdown a line holding nothing but `1987.` is an ordered list starting
 * at 1987 with one empty item — which renders as nothing at all. A year is the
 * likeliest whole answer to "when was this taken?", and a bare `-` or `1)` is
 * never meant as a list either, so such a list becomes a paragraph of its own
 * source. A list with any content in it is left alone.
 */
export function remarkEmptyLists() {
  return (tree: Root, file: { value: unknown }) => {
    restoreEmptyLists(tree, typeof file.value === 'string' ? file.value : '')
  }
}

/** Replaces every all-empty list under `parent`, recursively. */
function restoreEmptyLists(parent: { children: RootContent[] }, source: string): void {
  parent.children = parent.children.map((child) => {
    if (child.type === 'list' && child.children.every((item) => item.children.length === 0)) {
      const text = sourceOf(child, source)
      if (text !== null) {
        const paragraph: Paragraph = {
          type: 'paragraph',
          children: [{ type: 'text', value: text }],
        }
        return paragraph
      }
    }
    if ('children' in child) {
      restoreEmptyLists(child, source)
    }
    return child
  })
}

/** The source text a node was parsed from, trimmed, or null when unknown. */
function sourceOf(node: RootContent, source: string): string | null {
  const start = node.position?.start.offset
  const end = node.position?.end.offset
  if (start === undefined || end === undefined) {
    return null
  }
  return source.slice(start, end).trim()
}
