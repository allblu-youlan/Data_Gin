export type ApiField = {
  name: string
  type: string
  required: boolean
  description: string
  nullable?: boolean
  location?: string
  example?: unknown
  children?: ApiField[]
}
export type ApiOperation = {
  id: string
  method: string
  path: string
  title: string
  description: string
  permission: string
  notes?: { title: string; body: string }[]
  parentPermission?: string
  authentication: string
  requestFields: ApiField[]
  responseFields: ApiField[]
  requestExample: unknown
  responseExample: unknown
  errors: { status: number; code: string; description: string }[]
}
export type ApiGroup = { code: string; name: string; description?: string; children: { code: string; name: string; operations: ApiOperation[] }[] }
export type ApiDocument = { title: string; version: string; description?: string; groups: ApiGroup[] }

export function buildApiDocsLink(permission = ''): string {
  const query = new URLSearchParams({ view: 'api-docs' })
  if (permission.trim()) query.set('permission', permission.trim())
  return `?${query}`
}

export function parseApiDocument(payload: unknown): ApiDocument {
  const envelope = payload as { code?: number; data?: { document?: ApiDocument } } | null
  const document = envelope?.data?.document
  if (envelope?.code !== 0 || !document || !Array.isArray(document.groups)) throw new Error('接口文档加载失败，请稍后重试。')
  return document
}

function fieldText(fields: ApiField[]): string {
  return fields.map(field => `${field.name} ${field.type} ${field.description} ${fieldText(field.children ?? [])}`).join(' ')
}

export function filterApiGroups(groups: ApiGroup[], query: string): ApiGroup[] {
  const term = query.trim().toLocaleLowerCase()
  if (!term) return groups
  return groups.map(group => ({ ...group, children: group.children.map(child => ({ ...child, operations: child.operations.filter(op =>
    `${group.code} ${group.name} ${child.code} ${child.name} ${op.id} ${op.title} ${op.path} ${op.method} ${op.permission} ${op.parentPermission ?? ''} ${op.description} ${(op.notes ?? []).map(note => `${note.title} ${note.body}`).join(' ')} ${fieldText(op.requestFields)} ${fieldText(op.responseFields)}`.toLocaleLowerCase().includes(term),
  ) })).filter(child => child.operations.length > 0) })).filter(group => group.children.length > 0)
}

export function buildCurl(operation: ApiOperation, baseURL: string): string {
  const quote = (text: string) => `'${text.split("'").join( "'\"'\"'")}'`
  const path = operation.path.replace(/:([A-Za-z][A-Za-z0-9_]*)|\{([^}]+)\}/g, (placeholder, colonName: string | undefined, braceName: string | undefined) => {
    const name = colonName ?? braceName
    const field = operation.requestFields.find(item => item.location === 'path' && item.name === name)
    const example = field?.example ?? (name === 'id' ? 7 : undefined)
    return example === undefined ? placeholder : encodeURIComponent(String(example))
  })
  const query = new URLSearchParams()
  operation.requestFields.filter(field => field.location === 'query' && field.example !== undefined).forEach(field => query.append(field.name, String(field.example)))
  const url = `${baseURL.trim().replace(/\/+$/, '')}${path}${query.size ? `${path.includes('?') ? '&' : '?'}${query}` : ''}`
  const lines = [`curl --request ${operation.method} ${quote(url)}`]
  if (operation.permission) lines.push(`--header ${quote('token: dg_open_REPLACE_WITH_YOUR_TOKEN')}`)
  lines.push(`--header ${quote('Content-Type: application/json')}`)
  if (operation.method !== 'GET') lines.push(`--data-raw ${quote(JSON.stringify(operation.requestExample ?? {}, null, 2))}`)
  return lines.join(' \\\n  ')
}
