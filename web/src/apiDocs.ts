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

export function formatApiFieldType(type: string): string {
  const value = type.trim()
  const array = value.match(/^array<(.+)>$/i) ?? value.match(/^(.+)\[\]$/)
  if (array) {
    const itemType = formatApiFieldType(array[1])
    return `${itemType.includes(' | ') ? `(${itemType})` : itemType}[]`
  }
  const alternatives = value.split(/\s*(?:\/|\|)\s*/)
  if (alternatives.length > 1) return alternatives.map(formatApiFieldType).join(' | ')

  const primitive = value.replace(/\([^)]*\)/g, '').replace(/\s+unsigned$/i, '').trim().toLowerCase()
  if (/^(?:string|n?varchar2?|n?char|text|clob|date|datetime|timestamp)$/.test(primitive)) return 'string'
  if (/^(?:integer|u?int\d*|bigint|smallint|tinyint)$/.test(primitive)) return 'integer'
  if (/^(?:number|numeric|decimal|float\d*|double|real)$/.test(primitive)) return 'number'
  if (/^(?:boolean|bool)$/.test(primitive)) return 'boolean'
  if (primitive === 'object' || primitive === 'json' || /^[A-Z][A-Za-z0-9]*$/.test(value)) return 'object'
  return primitive
}

export function formatApiFieldDescription(description: string): string {
  return description.split('\n').map(line => {
    const value = line.trim()
    if (/^(?:Oracle源类型|数据库类型|SQL类型)[：:]/i.test(value)) return ''
    if (value === '精确filters：是') return '精确筛选：支持'
    if (value === '精确filters：否') return '精确筛选：不支持'
    const meaning = value.replace(/^(?:含义、关系|单位\/格式与使用说明|空值 \/ 可选性)[：:]\s*/, '')
      .replace(/源数据库NULL时返回/g, '为空时返回')
    if (/^(?:(?:字段必返|必返|开放接口必返)，)?不为 null$/.test(meaning)) return ''
    if (meaning === '源字符串' || meaning === '标识字符串') return ''
    return meaning
  }).filter(Boolean).join('\n')
}

function htmlText(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function markdownCell(value: string | number): string {
  return htmlText(String(value)).replace(/\\/g, '\\\\').replace(/\|/g, '\\|')
    .replace(/`/g, '\\`').replace(/\r\n?|\n/g, '<br>')
}

function markdownTable(headings: string[], rows: (string | number)[][]): string {
  const row = (cells: (string | number)[]) => `| ${cells.map(markdownCell).join(' | ')} |`
  return [row(headings), row(headings.map(() => '---')), ...rows.map(row)].join('\n')
}

function markdownCode(value: string, language: string): string {
  const longestFence = Math.max(0, ...(value.match(/`+/g) ?? []).map(run => run.length))
  const fence = '`'.repeat(Math.max(3, longestFence + 1))
  return `${fence}${language}\n${value}\n${fence}`
}

function markdownFields(fields: ApiField[], request: boolean): string {
  const rows: string[][] = []
  function visit(items: ApiField[], parent = '', inheritedLocation = request ? 'body' : '—') {
    for (const field of items) {
      const location = field.location ?? inheritedLocation
      if (request && location.toLowerCase() === 'header' && /^(?:token|content-type)$/i.test(field.name)) continue
      const type = formatApiFieldType(field.type)
      const array = type.includes('[]') || type === 'array'
      const path = `${parent ? `${parent}.` : ''}${field.name}${array ? '[]' : ''}`
      const displayType = field.nullable && !type.split(' | ').includes('null') ? `${type} | null` : type
      const description = formatApiFieldDescription(field.description)
      const annotations = request
        ? [field.required ? '必填' : '', location !== 'body' ? `${location} 参数` : '']
        : [!field.required && !description.includes('省略') ? '可能省略' : '']
      const annotation = annotations.filter(Boolean).join(' · ')
      rows.push([path, displayType, `${annotation ? `${annotation}。` : ''}${description}`])
      visit(field.children ?? [], path, location)
    }
  }
  visit(fields)
  if (!rows.length) return request ? '无需提供参数。' : '未提供响应字段说明。'
  return markdownTable(['字段', '类型', '含义'], rows)
}

function selectedApiGroups(document: ApiDocument, operationId?: string): ApiGroup[] {
  if (operationId === undefined) return document.groups
  const groups = document.groups.map(group => ({
    ...group,
    children: group.children.map(child => ({ ...child, operations: child.operations.filter(operation => operation.id === operationId) }))
      .filter(child => child.operations.length > 0),
  })).filter(group => group.children.length > 0)
  if (!groups.length) throw new Error('接口不存在，无法导出文档。')
  return groups
}

export function apiMarkdownFilename(document: ApiDocument, operationId?: string): string {
  selectedApiGroups(document, operationId)
  const safePart = (value: string) => value.replace(/[\p{Cc}\\/:*?"<>|]/gu, '-').replace(/\s+/g, '-').replace(/^[.-]+|[.-]+$/g, '')
  const title = safePart(document.title) || '开放接口文档'
  return [title, ...(operationId === undefined ? [] : [safePart(operationId)]), `v${safePart(document.version)}`].join('-') + '.md'
}

export function buildApiMarkdown(document: ApiDocument, baseURL: string, operationId?: string): string {
  const groups = selectedApiGroups(document, operationId)
  const operations = groups.flatMap(group => group.children.flatMap(child => child.operations))
  const authentication = [...new Set(operations.map(operation => operation.authentication.trim()).filter(Boolean))]
  const sections = [
    `# ${markdownCell(document.title)}`,
    `文档版本：${markdownCell(document.version)}`,
    ...(baseURL.trim() ? [`服务地址：${markdownCell(baseURL.trim().replace(/\/+$/, ''))}`] : []),
    ...(document.description?.trim() ? [document.description.trim()] : []),
    '## 授权说明',
    ...authentication,
    '通过请求头传入开放接口凭证，不加 Bearer 前缀。以下凭证为占位符，请替换为管理员提供的完整凭证。',
    markdownCode('token: dg_open_REPLACE_WITH_YOUR_TOKEN\nContent-Type: application/json', 'http'),
    '每个接口的权限码见对应说明；授予一级权限可覆盖其下的二级权限，数据访问范围仍然生效。',
  ]

  for (const group of groups) {
    sections.push(`## ${markdownCell(group.name)}`)
    if (group.description?.trim()) sections.push(group.description.trim())
    for (const child of group.children) {
      sections.push(`### ${markdownCell(child.name)}`)
      for (const operation of child.operations) {
        const hasRequestBody = operation.method.toUpperCase() !== 'GET' && operation.requestExample !== undefined && operation.requestExample !== null
        sections.push(
          `#### ${markdownCell(operation.title)}`,
          markdownCode(`${operation.method} ${operation.path}`, 'http'),
          operation.description.trim(),
          `接口权限：${markdownCell(operation.permission || '无需授权')}${operation.parentPermission ? `；一级权限：${markdownCell(operation.parentPermission)}` : ''}`,
          '##### 请求参数',
          markdownFields(operation.requestFields, true),
          ...(hasRequestBody ? ['##### 请求 JSON', markdownCode(JSON.stringify(operation.requestExample, null, 2), 'json')] : []),
          '##### cURL 示例',
          markdownCode(buildCurl(operation, baseURL), 'bash'),
          '##### 响应字段',
          markdownFields(operation.responseFields, false),
          '##### 响应 JSON',
          markdownCode(JSON.stringify(operation.responseExample ?? {}, null, 2), 'json'),
          '##### 错误说明',
          operation.errors.length ? markdownTable(['HTTP', '业务码', '说明与处理'], operation.errors.map(error => [error.status, error.code, error.description])) : '未提供额外错误说明。',
        )
        if (operation.notes?.length) {
          sections.push('##### 补充说明')
          for (const note of operation.notes) sections.push(`<details>\n<summary>${htmlText(note.title)}</summary>\n\n${note.body}\n\n</details>`)
        }
      }
    }
  }
  return sections.filter(Boolean).join('\n\n') + '\n'
}

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
