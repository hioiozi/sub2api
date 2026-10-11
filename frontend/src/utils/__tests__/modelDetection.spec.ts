import { describe, expect, it } from 'vitest'
import { extractHTML, sandboxDocument } from '../modelDetection'

describe('model detection output', () => {
  it('extracts a fenced HTML document', () => {
    expect(extractHTML('done\n```html\n<html><body>ok</body></html>\n```')).toContain('<html>')
    expect(extractHTML('No markup')).toBe('')
    expect(extractHTML('<html>truncated')).toBe('')
    expect(extractHTML('<svg></svg>trailing prose')).toBe('<svg></svg>')
  })
  it('removes executable elements and network-capable attributes', () => {
    const result = sandboxDocument('<html><body><script>alert(1)</script><svg onload="alert(1)"><image href="https://example.com/x"/></svg></body></html>')
    expect(result).not.toContain('<script')
    expect(result).not.toContain('onload=')
    expect(result).not.toContain('https://example.com/x')
    expect(result).toContain('Content-Security-Policy')
    expect(sandboxDocument('<svg><use href="#shape"/><use xlink:href="https://example.com"/></svg>')).toContain('href="#shape"')
    expect(sandboxDocument('<svg><use xlink:href="https://example.com"/></svg>')).not.toContain('https://example.com')
  })
})
