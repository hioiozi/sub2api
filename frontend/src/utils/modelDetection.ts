export function extractHTML(text: string): string {
  const fence = text.match(/```(?:html)?\s*([\s\S]*?)```/i)
  const source = (fence?.[1] || text).trim()
  const start = source.search(/<!doctype html|<html\b|<svg\b/i)
  if (start < 0) return ''
  const markup = source.slice(start)
  const closing = /^<svg\b/i.test(markup) ? '</svg>' : '</html>'
  const end = markup.toLowerCase().lastIndexOf(closing)
  if (end < 0) return ''
  const html = markup.slice(0, end + closing.length)
  return html.length <= 220 * 1024 ? html : ''
}

export function sandboxDocument(html: string): string {
  const parser = new DOMParser()
  const doc = parser.parseFromString(html, 'text/html')
  doc.querySelectorAll('script,iframe,object,embed,form,meta[http-equiv],link,base').forEach((node) => node.remove())
  doc.querySelectorAll('*').forEach((node) => {
    for (const attr of [...node.attributes]) {
      const name = attr.name.toLowerCase()
      const localReference = ['href', 'xlink:href'].includes(name) && /^#[a-zA-Z_][\w:.-]*$/.test(attr.value)
      if (/^on/i.test(name) || (!localReference && ['href', 'xlink:href', 'src', 'srcset', 'action', 'formaction', 'poster', 'ping'].includes(name))) {
        node.removeAttribute(attr.name)
      }
    }
  })
  const policy = doc.createElement('meta')
  policy.httpEquiv = 'Content-Security-Policy'
  policy.content = "default-src 'none'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:"
  doc.head.prepend(policy)
  return '<!doctype html>' + doc.documentElement.outerHTML
}
