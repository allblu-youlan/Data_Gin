import { useEffect, useMemo, useState } from 'react'
import { BookOpen, Check, ChevronRight, Copy, Search } from 'lucide-react'
import { apiURL } from './apiURL'
import { buildCurl, filterApiGroups, parseApiDocument, type ApiDocument, type ApiField, type ApiOperation } from './apiDocs'
import styles from './ApiDocsPage.module.css'

const baseURL = import.meta.env.VITE_API_BASE_URL ?? ''
const title = 'allblu开放接口文档'

export default function ApiDocsPage() {
  const [document, setDocument] = useState<ApiDocument | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [query, setQuery] = useState(() => new URLSearchParams(window.location.search).get('permission') ?? '')
  const [selected, setSelected] = useState(() => decodeURIComponent(window.location.hash.slice(1)))
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
  const matches = groups.flatMap(group => group.children.flatMap(child => child.operations.map(operation => ({ group, child, operation }))))
  const current = matches.find(item => item.operation.id === selected) ?? matches[0]
  const count = groups.reduce((sum, group) => sum + group.children.reduce((n, child) => n + child.operations.length, 0), 0)
  return <div className={styles.page}>
    <a className={styles.skip} href="#api-detail">跳到接口详情</a>
    <header className={styles.header}><a href="?view=api-docs" className={styles.brand}><BookOpen size={22} aria-hidden="true" /><strong>{title}</strong></a><span className={styles.headerMeta}>API REFERENCE <span>{document ? `v${document.version}` : '加载中'}</span></span></header>
    <div className={styles.layout}>
      <aside className={styles.sidebar} aria-label="接口目录">
        <label className={styles.search}><Search size={17} aria-hidden="true" /><input type="search" value={query} onChange={event => setQuery(event.currentTarget.value)} placeholder="搜索接口、路径、字段或权限码" aria-label="搜索接口、路径、字段或权限码" /></label>
        <div className={styles.directoryTitle}>接口目录 <span>{count} 个接口</span></div>
        <nav>{groups.map(group => <section className={styles.navGroup} key={group.code}><h2>{group.name}</h2>{group.children.map(child => <details key={child.code} open><summary>{child.name}<span>{child.operations.length}</span></summary><div>{child.operations.map(op => <a key={op.id} href={`#${encodeURIComponent(op.id)}`} aria-current={current?.operation.id === op.id ? 'page' : undefined}><span className={styles.methodSmall}>{op.method}</span><span>{op.title}</span></a>)}</div></details>)}</section>)}</nav>
        {!count && document && <p className={styles.muted}>未找到匹配接口，试试字段名称、路径或权限码。</p>}
      </aside>
      <main id="api-detail" className={styles.main} tabIndex={-1}>
        {error ? <div className={styles.feedback} role="alert"><h1>文档暂时不可用</h1><p>{error}</p><button onClick={() => setAttempt(value => value + 1)}>重新加载</button></div> : !document ? <div className={styles.feedback} role="status">正在加载接口文档…</div> : current ? <>
          <div className={styles.breadcrumb}>{current.group.name}<ChevronRight size={14} aria-hidden="true" />{current.child.name}</div>
          <OperationDetail key={current.operation.id} operation={current.operation} />
        </> : query.trim() ? <div className={styles.feedback}><h1>未找到匹配接口</h1><p>试试字段名称、路径或权限码。</p><button onClick={() => setQuery('')}>清除搜索</button></div> : <div className={styles.feedback}>当前没有已发布的接口文档。</div>}
      </main>
    </div>
  </div>
}

function OperationDetail({ operation: op }: { operation: ApiOperation }) {
  return <article>
    <div className={styles.eyebrow}>接口参考</div><h1>{op.title}</h1>
    <div className={styles.endpoint}><strong>{op.method}</strong><code>{op.path}</code><CopyButton text={op.path} label="复制路径" /></div>
    <p className={styles.description}>{op.description}</p>
    <section className={styles.auth} aria-label="调用认证"><h2>认证与权限</h2><p>{op.authentication}</p><p>接口权限 <code>{op.permission}</code>{op.parentPermission && <> · 授予一级权限 <code>{op.parentPermission}</code> 即可覆盖</>}</p></section>
    {!!op.notes?.length && <OperationNotes notes={op.notes} />}
    <FieldSection title="请求参数" fields={op.requestFields} />
    <CodeBlock title="请求示例 · cURL" value={buildCurl(op, new URL(baseURL || '/', window.location.origin).href)} raw />
    <FieldSection title="响应字段" fields={op.responseFields} />
    <CodeBlock title="响应示例" value={op.responseExample} />
    <section className={styles.section}><h2>错误说明</h2><div className={styles.tableScroll}><table><thead><tr><th>HTTP</th><th>业务码</th><th>说明与处理</th></tr></thead><tbody>{op.errors.map((error, index) => <tr key={`${error.status}-${error.code}-${index}`}><td>{error.status}</td><td><code>{error.code}</code></td><td>{error.description}</td></tr>)}</tbody></table></div></section>
  </article>
}

function OperationNotes({ notes }: { notes: NonNullable<ApiOperation['notes']> }) {
  const [expanded, setExpanded] = useState(false)
  const [revision, setRevision] = useState(0)
  return <section className={styles.section} aria-label="使用规则与字段说明">
    <div className={styles.sectionHeading}><h2>使用规则与字段说明</h2><div><button onClick={() => { setExpanded(true); setRevision(n => n + 1) }}>全部展开</button><button onClick={() => { setExpanded(false); setRevision(n => n + 1) }}>全部收起</button></div></div>
    <div className={styles.notes} key={revision}>{notes.map((note, index) => <details className={styles.note} key={`${index}-${note.title}`} open={expanded}><summary>{note.title}</summary><div className={styles.noteBody}>{note.body}</div></details>)}</div>
  </section>
}

function FieldSection({ title, fields }: { title: string; fields: ApiField[] }) {
  const [expanded, setExpanded] = useState(false)
  const [revision, setRevision] = useState(0)
  return <section className={styles.section} aria-label={title}><div className={styles.sectionHeading}><h2>{title}</h2><div><button onClick={() => { setExpanded(true); setRevision(n => n + 1) }}>全部展开</button><button onClick={() => { setExpanded(false); setRevision(n => n + 1) }}>全部收起</button></div></div>{fields.length ? <FieldList key={revision} fields={fields} expanded={expanded} depth={0} /> : <p className={styles.muted}>无需提供参数。</p>}</section>
}

function FieldList({ fields, expanded, depth }: { fields: ApiField[]; expanded: boolean; depth: number }) {
  const [showAll, setShowAll] = useState(expanded)
  const visible = showAll ? fields : fields.slice(0, 8)
  return <div className={styles.fields}>{visible.map(field => <div className={styles.field} key={field.name}>
    <div className={styles.fieldHeading}><code>{field.name}</code><span>{field.type}</span>{field.required && <em>{depth === 0 && field.location ? '必填' : '必需'}</em>}{field.nullable && <span>可为 null</span>}{field.location && <span>{field.location}</span>}</div>
    <p>{field.description}</p>{field.example !== undefined && <div className={styles.fieldExample}>示例：<code>{typeof field.example === 'string' ? field.example : JSON.stringify(field.example)}</code></div>}
    {!!field.children?.length && <details open={expanded}><summary>子字段 <span>{field.children.length}</span></summary><FieldList fields={field.children} expanded={expanded} depth={depth + 1} /></details>}
  </div>)}{fields.length > 8 && <button className={styles.more} onClick={() => setShowAll(value => !value)} aria-expanded={showAll}>{showAll ? '收起字段' : `展开其余 ${fields.length - 8} 个字段`}<span>共 {fields.length} 个字段</span></button>}</div>
}

function CodeBlock({ title, value, raw = false }: { title: string; value: unknown; raw?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const text = raw ? String(value) : JSON.stringify(value, null, 2) ?? '{}'
  const long = text.split('\n').length > 12
  return <section className={styles.section}><div className={styles.sectionHeading}><h2>{title}</h2><CopyButton text={text} label="复制示例" /></div><pre className={!expanded && long ? styles.collapsedCode : undefined}><code>{text}</code></pre>{long && <button className={styles.more} onClick={() => setExpanded(value => !value)} aria-expanded={expanded}>{expanded ? '收起示例' : '展开完整示例'}</button>}</section>
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [status, setStatus] = useState('')
  async function copy() { try { await navigator.clipboard.writeText(text); setStatus('已复制') } catch { setStatus('复制失败，请手动选择') } }
  return <button className={styles.copy} onClick={() => void copy()} aria-label={label}>{status === '已复制' ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}<span aria-live="polite">{status || label}</span></button>
}
