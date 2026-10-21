export function setDocumentMeta(title: string, description = '', keywords = '') {
  document.title = title
  setMeta('description', description)
  setMeta('keywords', keywords)
  setMeta('og:title', title, 'property')
  setMeta('og:description', description, 'property')
}

function setMeta(name: string, content: string, attribute = 'name') {
  let element = document.head.querySelector(`meta[${attribute}="${name}"]`)
  if (!(element instanceof HTMLMetaElement)) {
    element = document.createElement('meta')
    element.setAttribute(attribute, name)
    document.head.appendChild(element)
  }
  element.setAttribute('content', content)
}
