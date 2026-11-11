import { useEffect, useMemo, useRef, useState } from 'react'
import { BookOpen, Check, ChevronDown, ChevronRight, Copy, KeyRound, Menu, Search, Terminal, X } from 'lucide-react'
import { apiURL } from './apiURL'
import { Brand } from './components/Brand'
import { buildCurl, filterApiFields, filterApiGroups, parseApiDocument, type ApiDocument, type ApiField, type ApiOperation } from './apiDocs'
import styles from './ApiDocsPage.module.css'

const baseURL = import.meta.env.VITE_API_BASE_URL ?? ''
const title = 'allblu开放接口文档'

export default function ApiDocsPage() {
  const [document, setDocument] = useState<ApiDocument | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [query, setQuery] = useState('')
  const [directoryOpen, setDirectoryOpen] = useState(false)
  const [selected, setSelected] = useState(() => decodeURIComponent(window.location.hash.slice(1)))
  const detailRef = useRef<HTMLElement>(null)
  const previousOperation = useRef<string>()
  useEffect(() => {
    window.document.title = title
    const onHash = () => setSelected(decodeURIComponent(window.location.hash.slice(1)))
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    const timeout = window.setTimeout(() => controller.abort('timeout'), 20000)
    setError('')
    fetch(apiURL('/v1/api-docs', baseURL), { signal: controller.signal, credentials: 'omit' })
      .then(async response => {
        if (!response.ok) throw new Error('接口文档暂时无法加载，请稍后重试。')
        setDocument(parseApiDocument(await response.json()))
      })
      .catch((cause: unknown) => { if (!controller.signal.aborted || controller.signal.reason === 'timeout') setError(cause instanceof Error && !controller.signal.aborted ? cause.message : '加载超时，请重试。') })
      .finally(() => window.clearTimeout(timeout))
    return () => { controller.abort(); window.clearTimeout(timeout) }
  }, [attempt])
  const groups = useMemo(() => filterApiGroups(document?.groups ?? [], query), [document, query])
  const all = document?.groups.flatMap(group => group.children.flatMap(child => child.operations.map(operation => ({ group, child, operation })))) ?? []
  const current = all.find(item => item.operation.id === selected) ?? all[0]
  const currentOperationId = current?.operation.id
  useEffect(() => {
    if (previousOperation.current && previousOperation.current !== currentOperationId) {
      detailRef.current?.scrollIntoView({ block: 'start' })
    }
    previousOperation.current = currentOperationId
  }, [currentOperationId])
  const count = groups.reduce((sum, group) => sum + group.children.reduce((n, child) => n + child.operations.length, 0), 0)
  return <div className={styles.page}>
    <a className={styles.skip} href="#api-detail" onClick={event => {
      event.preventDefault()
      detailRef.current?.focus({ preventScroll: true })
      detailRef.current?.scrollIntoView({ block: 'start' })
    }}>跳到接口详情</a>
    <header className={styles.header}>
      <a href="?view=api-docs" className={styles.brand} aria-label={title}><Brand size="medium" /><span className={styles.brandDivider} /><strong>开放接口文档</strong></a>
      <span className={styles.headerMeta}><BookOpen size={16} aria-hidden="true" /> API Reference <span>v{document?.version ?? '2.0'}</span></span>
      <button className={styles.mobileMenu} aria-expanded={directoryOpen} aria-controls="api-directory" onClick={() => setDirectoryOpen(value => !value)}>{directoryOpen ? <X size={18} aria-hidden="true" /> : <Menu size={18} aria-hidden="true" />}目录</button>
    </header>
    <div className={styles.layout}>
      <aside id="api-directory" className={`${styles.sidebar} ${directoryOpen ? styles.directoryOpen : ''}`} aria-label="接口目录">
        <label className={styles.search}><Search size={17} aria-hidden="true" /><input type="search" value={query} onChange={event => setQuery(event.currentTarget.value)} placeholder="搜索接口、路径或字段" aria-label="搜索接口、路径或字段" /></label>
        <div className={styles.directoryTitle}>接口目录 <span aria-live="polite">{count} 个接口</span></div>
        <nav>{groups.map(group => <section className={styles.navGroup} key={group.code}><h2>{group.name}</h2>{group.children.map(child => <details key={child.code} open><summary><ChevronRight size={14} aria-hidden="true" />{child.name}<span>{child.operations.length}</span></summary><div>{child.operations.map(op => <a key={op.id} href={`#${encodeURIComponent(op.id)}`} onClick={() => { setDirectoryOpen(false); if (window.matchMedia('(max-width: 760px)').matches) detailRef.current?.focus({ preventScroll: true }) }} aria-current={current?.operation.id === op.id ? 'page' : undefined}><span className={styles.methodSmall}>{op.method}</span><span>{op.title}</span></a>)}</div></details>)}</section>)}</nav>
        {!count && document && <p className={styles.muted}>未找到匹配接口，试试字段名称或路径。</p>}
      </aside>
      <main id="api-detail" ref={detailRef} className={styles.main} tabIndex={-1}>
        {error ? <div className={styles.feedback} role="alert"><h1>文档暂时不可用</h1><p>{error}</p><button onClick={() => setAttempt(value => value + 1)}>重新加载</button></div> : !document ? <div className={styles.feedback} role="status">正在加载接口文档…</div> : current ? <>
          <div className={styles.breadcrumb}>{current.group.name}<ChevronRight size={14} aria-hidden="true" />{current.child.name}</div>
          <OperationDetail key={current.operation.id} operation={current.operation} />
        </> : <div className={styles.feedback}>当前没有已发布的接口文档。</div>}
      </main>
    </div>
  </div>
}

function OperationDetail({ operation: op }: { operation: ApiOperation }) {
  const sections = [
    { id: 'request-fields', label: '请求参数' },
    { id: 'request-example', label: '请求示例' },
    { id: 'response-fields', label: '响应字段' },
    { id: 'response-example', label: '响应示例' },
    ...(op.notes?.length ? [{ id: 'usage-notes', label: '使用规则' }] : []),
    { id: 'error-reference', label: '错误说明' },
  ]
  return <div className={styles.detailLayout}><article className={styles.article}>
    <div className={styles.eyebrow}>API REFERENCE</div><h1>{op.title}</h1>
    <p className={styles.description}>{op.description}</p>
    <div className={styles.endpoint}><strong>{op.method}</strong><code>{op.path}</code><CopyButton text={op.path} label="复制路径" /></div>
    <section className={styles.auth} aria-label="调用认证"><KeyRound size={18} aria-hidden="true" /><div><h2>认证与权限</h2><p>{op.authentication}</p><p className={styles.permission}>接口权限 <code>{op.permission}</code>{op.parentPermission && <><br />授予一级权限 <code>{op.parentPermission}</code> 即可覆盖</>}</p></div></section>
    <FieldSection id="request-fields" title="请求参数" fields={op.requestFields} kind="request" />
    <CodeBlock id="request-example" title="请求示例 · cURL" value={buildCurl(op, new URL(baseURL || '/', window.location.origin).href)} raw />
    <FieldSection id="response-fields" title="响应字段" fields={op.responseFields} kind="response" />
    <CodeBlock id="response-example" title="响应示例" value={op.responseExample} />
    {!!op.notes?.length && <OperationNotes notes={op.notes} />}
    <section id="error-reference" tabIndex={-1} className={styles.section}><div className={styles.sectionHeading}><h2>错误说明</h2></div><div className={styles.tableScroll}><table><thead><tr><th>HTTP</th><th>业务码</th><th>说明与处理</th></tr></thead><tbody>{op.errors.map((error, index) => <tr key={`${error.status}-${error.code}-${index}`}><td>{error.status}</td><td><code>{error.code}</code></td><td>{error.description}</td></tr>)}</tbody></table></div></section>
    <footer className={styles.footer}><BookOpen size={16} aria-hidden="true" />{title}</footer>
  </article><nav className={styles.pageNav} aria-label="本页内容"><span>本页内容</span>{sections.map(section => <button key={section.id} onClick={() => {
    const target = window.document.getElementById(section.id)
    target?.focus({ preventScroll: true })
    target?.scrollIntoView({ block: 'start' })
  }}>{section.label}</button>)}</nav></div>
}

function OperationNotes({ notes }: { notes: NonNullable<ApiOperation['notes']> }) {
  const [expanded, setExpanded] = useState(false)
  const [revision, setRevision] = useState(0)
  return <section id="usage-notes" tabIndex={-1} className={styles.section} aria-label="使用规则与字段说明">
    <div className={styles.sectionHeading}><h2>使用规则与字段说明</h2><div><button onClick={() => { setExpanded(true); setRevision(n => n + 1) }}>全部展开</button><button onClick={() => { setExpanded(false); setRevision(n => n + 1) }}>全部收起</button></div></div>
    <div className={styles.notes} key={revision}>{notes.map((note, index) => <details className={styles.note} key={`${index}-${note.title}`} open={expanded}><summary><ChevronRight size={16} aria-hidden="true" />{note.title}</summary><div className={styles.noteBody}>{note.body}</div></details>)}</div>
  </section>
}

function FieldSection({ id, title, fields, kind }: { id: string; title: string; fields: ApiField[]; kind: 'request' | 'response' }) {
  const [expanded, setExpanded] = useState(false)
  const [revision, setRevision] = useState(0)
  const [query, setQuery] = useState('')
  const filtered = useMemo(() => filterApiFields(fields, query), [fields, query])
  const searching = query.trim().length > 0
  return <section id={id} tabIndex={-1} className={styles.section} aria-label={title}>
    <div className={styles.sectionHeading}><h2>{title}</h2><div><button onClick={() => { setExpanded(true); setRevision(n => n + 1) }}>全部展开</button><button onClick={() => { setQuery(''); setExpanded(false); setRevision(n => n + 1) }}>全部收起</button></div></div>
    {!!fields.length && <div className={styles.fieldToolbar}><label className={styles.fieldSearch}><Search size={16} aria-hidden="true" /><input type="search" value={query} onChange={event => setQuery(event.currentTarget.value)} aria-label={`搜索${title}`} placeholder="查找字段或说明…" /></label><span aria-live="polite">{searching ? (filtered.length ? '已展开匹配字段' : '没有匹配字段') : '支持展开查看子字段'}</span></div>}
    {filtered.length ? <div className={styles.fieldTable}><div className={styles.fieldColumns} aria-hidden="true"><span>字段 / 类型</span><span>说明与示例</span></div><FieldList key={`${revision}-${query}`} fields={filtered} expanded={expanded || searching} kind={kind} /></div> : <p className={styles.emptyFields}>{searching ? '没有找到匹配字段，试试字段名称、完整路径或说明。' : '无需提供参数。'}</p>}
  </section>
}

function FieldList({ fields, expanded, kind, parentPath = '' }: { fields: ApiField[]; expanded: boolean; kind: 'request' | 'response'; parentPath?: string }) {
  const [showAll, setShowAll] = useState(expanded)
  const visible = showAll ? fields : fields.slice(0, 8)
  const locations: Record<string, string> = { header: '请求头', body: '请求体', query: '查询参数', path: '路径参数' }
  return <div className={styles.fields}>{visible.map(field => {
    const path = parentPath ? `${parentPath}.${field.name}` : field.name
    return <div className={styles.field} key={field.name}>
      <div className={styles.fieldRow}><div className={styles.fieldIdentity}>
        <code className={styles.fieldName} title={path}>{field.name}</code>
        <div className={styles.fieldMeta}><span className={styles.fieldType}>{field.type}</span>{field.required && <em>{kind === 'request' ? '必填' : '必返'}</em>}{field.nullable && <span>可为 null</span>}</div>
        {field.location && <span className={styles.fieldLocation}>{locations[field.location] ?? field.location}</span>}
      </div><div className={styles.fieldDescription}><p>{field.description}</p>{field.example !== undefined && <div className={styles.fieldExample}><span>示例</span><code>{typeof field.example === 'string' ? field.example : JSON.stringify(field.example)}</code></div>}</div></div>
      {!!field.children?.length && <details className={styles.childFields} open={expanded}><summary><ChevronRight size={15} aria-hidden="true" />子字段 <span>{field.children.length}</span><code>{path}{field.type.includes('array') || field.type.includes('[]') ? '[]' : ''}</code></summary><FieldList fields={field.children} expanded={expanded} kind={kind} parentPath={`${path}${field.type.includes('array') || field.type.includes('[]') ? '[]' : ''}`} /></details>}
    </div>
  })}{fields.length > 8 && <button className={styles.more} onClick={() => setShowAll(value => !value)} aria-expanded={showAll}><ChevronDown size={15} aria-hidden="true" className={showAll ? styles.rotate : undefined} />{showAll ? '收起字段' : `展开其余 ${fields.length - 8} 个字段`}<span>共 {fields.length} 个字段</span></button>}</div>
}

function CodeBlock({ id, title, value, raw = false }: { id: string; title: string; value: unknown; raw?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const text = raw ? String(value) : JSON.stringify(value, null, 2) ?? '{}'
  const long = text.split('\n').length > 12
  return <section id={id} tabIndex={-1} className={styles.section}><div className={styles.sectionHeading}><h2>{title}</h2></div><div className={styles.codePanel}><div className={styles.codeToolbar}><span><Terminal size={16} aria-hidden="true" />{raw ? 'cURL' : 'JSON'}<span className={styles.lineCount}>{text.split('\n').length} 行</span></span><CopyButton text={text} label="复制示例" /></div><pre tabIndex={0} aria-label={title} className={!expanded && long ? styles.collapsedCode : undefined}><code>{text}</code></pre>{long && <button className={styles.more} onClick={() => setExpanded(value => !value)} aria-expanded={expanded}><ChevronDown size={15} aria-hidden="true" className={expanded ? styles.rotate : undefined} />{expanded ? '收起示例' : '展开完整示例'}</button>}</div></section>
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [status, setStatus] = useState('')
  async function copy() { try { await navigator.clipboard.writeText(text); setStatus('已复制') } catch { setStatus('复制失败，请手动选择') } }
  return <button className={styles.copy} onClick={() => void copy()} aria-label={label}>{status === '已复制' ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}<span aria-live="polite">{status || label}</span></button>
}
