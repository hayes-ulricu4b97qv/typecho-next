import DOMPurify from 'dompurify'
import { marked } from 'marked'

export function renderMarkdown(source: string): string {
  return DOMPurify.sanitize(String(marked.parse(source, { async: false })))
}
