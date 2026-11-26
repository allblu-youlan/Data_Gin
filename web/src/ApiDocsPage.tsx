import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, BookOpen, Check, ChevronRight, Copy, KeyRound, Menu, Search, X } from 'lucide-react'
import { Button } from './components/ui/button'
import { Input } from './components/ui/input'
import { Badge } from './components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './components/ui/table'
import { apiURL } from './apiURL'
import { buildCurl, filterApiGroups, formatApiFieldDescription, formatApiFieldType, parseApiDocument, type ApiDocument, type ApiField, type ApiOperation } from './apiDocs'
import styles from './ApiDocsPage.module.css'

const baseURL = import.meta.env.VITE_API_BASE_URL ?? ''
const title = 'allblu 开放接口文档'
const authID = 'authentication'
const readHash = () => decodeURIComponent(window.location.hash.slice(1))
const operationLink = (id: string, section = '') => `#${encodeURIComponent(id)}${section ? `/${section}` : ''}`

export default function ApiDocsPage() {
  const [document, setDocument] = useState<ApiDocument | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [query, setQuery] = useState(() => new URLSearchParams(window.location.search).get('permission') ?? '')
  const [route, setRoute] = useState(readHash)
  const [menuOpen, setMenuOpen] = useState(false)
  const [selected, section] = route.split('/')
  const mainRef = useRef<HTMLElement>(null)

  useEffect(() => {
    window.document.title = title
    const onHash = () => { setRoute(readHash()); setMenuOpen(false) }
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
  const isAuth = selected === authID
  const index = matches.findIndex(item => item.operation.id === current?.operation.id)
  const currentID = isAuth ? authID : current?.operation.id

  useEffect(() => {
    if (!document) return
    if (section) window.document.getElementById(section)?.scrollIntoView({ block: 'start' })
    else window.scrollTo({ top: 0 })
  }, [currentID, section, document])

  function closeMenu() {
    setMenuOpen(false)
    mainRef.current?.focus({ preventScroll: true })
    window.scrollTo({ top: 0 })
  }

  return <div className={styles.page}>
    <a className={styles.skip} href="#api-detail" onClick={event => { event.preventDefault(); mainRef.current?.focus() }}>跳到文档内容</a>
    <header className={styles.header}>
      <a href="?view=api-docs" className={styles.brand}><BookOpen size={21} aria-hidden="true" /><strong>allblu</strong><span>开放接口文档</span></a>
      <div className={styles.headerMeta}><span>API Reference</span>{document && <Badge variant="outline">v{document.version}</Badge>}<Button variant="ghost" size="icon" className={styles.menuButton} aria-label={menuOpen ? '关闭接口目录' : '打开接口目录'} aria-expanded={menuOpen} aria-controls="api-directory" onClick={() => setMenuOpen(value => !value)}>{menuOpen ? <X aria-hidden="true" /> : <Menu aria-hidden="true" />}</Button></div>
    </header>
    <div className={styles.layout}>
      <aside id="api-directory" className={`${styles.sidebar} ${menuOpen ? styles.sidebarOpen : ''}`} aria-label="接口目录">
        <div className={styles.search}><Search size={16} aria-hidden="true" /><Input type="search" name="api-search" autoComplete="off" spellCheck={false} value={query} onChange={event => setQuery(event.currentTarget.value)} placeholder="搜索接口、字段…" aria-label="搜索接口、路径、字段或权限码" /></div>
        <nav aria-label="接入指南" className={styles.guideNav}><span className={styles.directoryTitle}>接入指南</span><a href={operationLink(authID)} aria-current={isAuth ? 'page' : undefined} onClick={closeMenu}><KeyRound size={16} aria-hidden="true" />授权说明<ChevronRight size={14} aria-hidden="true" /></a></nav>
        <div className={styles.directoryHeading}><span className={styles.directoryTitle}>接口参考</span><span aria-live="polite">{matches.length}</span></div>
        <nav aria-label="业务接口">{groups.map(group => <section className={styles.navGroup} key={group.code}><h2>{group.name}</h2>{group.children.map(child => <details key={child.code} open><summary>{child.name}<span>{child.operations.length}</span></summary><div>{child.operations.map(op => <a key={op.id} href={operationLink(op.id)} aria-current={!isAuth && current?.operation.id === op.id ? 'page' : undefined} onClick={closeMenu}><span className={styles.methodSmall} data-method={op.method}>{op.method}</span><span>{op.title}</span></a>)}</div></details>)}</section>)}</nav>
        {!matches.length && document && <div className={styles.noMatches}><p>没有匹配的接口</p><Button variant="ghost" size="sm" onClick={() => setQuery('')}>清除搜索</Button></div>}
      </aside>
      <main id="api-detail" ref={mainRef} className={styles.main} tabIndex={-1}>
        {error ? <div className={styles.feedback} role="alert"><h1>文档暂时不可用</h1><p>{error}</p><Button variant="outline" onClick={() => setAttempt(value => value + 1)}>重新加载</Button></div> : !document ? <div className={styles.feedback} role="status">正在加载接口文档…</div> : isAuth ? <Authorization document={document} onNavigate={() => setQuery('')} /> : current ? <>
          <div className={styles.breadcrumb}>{current.group.name}<ChevronRight size={13} aria-hidden="true" />{current.child.name}</div>
          <OperationDetail key={current.operation.id} operation={current.operation} />
          <nav className={styles.pagination} aria-label="相邻接口">{index > 0 ? <a href={operationLink(matches[index - 1].operation.id)}><ArrowLeft size={16} aria-hidden="true" /><span><small>上一个接口</small>{matches[index - 1].operation.title}</span></a> : <span />}{index < matches.length - 1 && <a href={operationLink(matches[index + 1].operation.id)}><span><small>下一个接口</small>{matches[index + 1].operation.title}</span><ArrowRight size={16} aria-hidden="true" /></a>}</nav>
        </> : query.trim() ? <div className={styles.feedback}><Search size={28} aria-hidden="true" /><h1>未找到匹配接口</h1><p>试试接口名称、路径或字段名称。</p><Button variant="outline" onClick={() => setQuery('')}>清除搜索</Button></div> : <div className={styles.feedback}>当前没有已发布的接口文档。</div>}
      </main>
    </div>
  </div>
}

function Authorization({ document, onNavigate }: { document: ApiDocument; onNavigate: () => void }) {
  return <article className={styles.authorization}>
    <div className={styles.breadcrumb}>接入指南<ChevronRight size={13} aria-hidden="true" />授权说明</div>
    <h1>授权说明</h1><p className={styles.description}>使用开放接口凭证调用业务接口。</p>
    <section className={styles.section}><h2>1. 获取凭证</h2><p>联系管理员创建开放接口账号，获取以 <code>dg_open_</code> 开头的完整凭证，并开通所需接口权限与数据范围。</p></section>
    <section className={styles.section}><h2>2. 添加请求头</h2><p>将凭证放入 <code>token</code> 请求头，不加 <code>Bearer</code> 前缀。</p><CodeBlock title="通用请求头" language="HTTP" text={'token: dg_open_REPLACE_WITH_YOUR_TOKEN\nContent-Type: application/json'} /></section>
    <section className={styles.section}><h2>3. 确认接口权限</h2><p>一级权限覆盖所属二级接口；商场、店铺等数据范围仍按账号配置生效。</p>
      {document.groups.map(group => <div key={group.code} className={styles.permissionGroup}><h3>{group.name}<code>{group.code}</code></h3>{group.children.map(child => <div key={child.code} className={styles.permissionRow}><span>{child.name}</span><code>{child.code}</code><a href={operationLink(child.operations[0].id)} onClick={onNavigate}>查看接口<ArrowRight size={13} aria-hidden="true" /></a></div>)}</div>)}
    </section>
    <section className={styles.section}><h2>授权失败</h2><p><code>401</code>：检查凭证是否完整、有效。<br /><code>403</code>：检查接口权限及账号数据范围。</p></section>
  </article>
}

function OperationDetail({ operation: op }: { operation: ApiOperation }) {
  const fields = op.requestFields.filter(field => !(field.location === 'header' && ['token', 'content-type'].includes(field.name.toLowerCase())))
  const summary = op.description.split('\n')[0]
  const requestURL = new URL(baseURL || '/', window.location.origin).href
  return <article>
    <header className={styles.operationHeader}><h1>{op.title}</h1>{summary !== op.title && <p className={styles.description}>{summary}</p>}
      <div className={styles.endpoint}><Badge variant="secondary">{op.method}</Badge><code>{op.path}</code><CopyButton text={op.path} label="复制路径" iconOnly /></div>
      <div className={styles.operationMeta}><span>application/json</span><a href={operationLink(authID)}><KeyRound size={13} aria-hidden="true" />授权说明<ChevronRight size={12} aria-hidden="true" /></a></div>
    </header>
    <nav className={styles.sectionNav} aria-label="本页内容">{[['request', '请求参数'], ['response', '响应字段'], ['errors', '错误说明'], ['examples', '调用示例']].map(([id, label]) => <a key={id} href={operationLink(op.id, id)} onClick={() => window.document.getElementById(id)?.scrollIntoView({ block: 'start' })}>{label}</a>)}</nav>
    <div className={styles.operationGrid}>
      <div className={styles.contract}>
        <FieldSection id="request" title="请求参数" fields={fields} request />
        <FieldSection id="response" title="响应字段" fields={op.responseFields} />
        <section id="errors" className={styles.section}><h2>错误说明</h2><Table><TableHeader><TableRow><TableHead>HTTP</TableHead><TableHead>业务码</TableHead><TableHead>含义与处理</TableHead></TableRow></TableHeader><TableBody>{op.errors.map((error, i) => <TableRow key={`${error.status}-${error.code}-${i}`}><TableCell>{error.status}</TableCell><TableCell><code>{error.code}</code></TableCell><TableCell>{error.description}</TableCell></TableRow>)}</TableBody></Table></section>
        {(!!op.notes?.length || summary !== op.description) && <section className={styles.section}><h2>补充说明</h2><div className={styles.notes}>{summary !== op.description && <details><summary>完整接口说明</summary><p>{op.description}</p></details>}{op.notes?.map((note, index) => <details key={`${index}-${note.title}`}><summary>{note.title}</summary><p>{note.body}</p></details>)}</div></section>}
      </div>
      <aside id="examples" className={styles.examples} aria-label="调用示例"><div className={styles.examplesInner}><h2>调用示例</h2><CodeBlock title="请求示例" language="cURL" text={buildCurl(op, requestURL)} /><CodeBlock title="响应示例" language="JSON" text={JSON.stringify(op.responseExample, null, 2) ?? '{}'} /></div></aside>
    </div>
  </article>
}

function FieldSection({ id, title, fields, request = false }: { id: string; title: string; fields: ApiField[]; request?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const [revision, setRevision] = useState(0)
  const expandable = fields.length > 8 || fields.some(field => field.children?.length)
  return <section id={id} className={styles.section} aria-label={title}>
    <div className={styles.sectionHeading}><h2>{title}<span>{fields.length}</span></h2>{expandable && <Button variant="ghost" size="sm" aria-expanded={expanded} onClick={() => { setExpanded(value => !value); setRevision(value => value + 1) }}>{expanded ? '全部收起' : '全部展开'}</Button>}</div>
    {fields.length ? <><div className={styles.fieldColumns}><span>字段 / 类型</span><span>含义</span></div><FieldList key={revision} fields={fields} expanded={expanded} request={request} /></> : <p className={styles.muted}>无需提供参数。</p>}
  </section>
}

function FieldList({ fields, expanded, request }: { fields: ApiField[]; expanded: boolean; request: boolean }) {
  const [showAll, setShowAll] = useState(expanded)
  const visible = showAll ? fields : fields.slice(0, 8)
  return <div className={styles.fields}>{visible.map(field => <div className={styles.field} key={field.name}>
    <div className={styles.fieldRow}><div className={styles.fieldHeading}><code>{field.name}</code><div><span>{formatApiFieldType(field.type)}</span>{request && field.required && <em>必填</em>}{field.nullable && !field.type.includes('null') && <span>可为空</span>}{request && field.location && field.location !== 'body' && <span>{field.location}</span>}</div></div><p>{formatApiFieldDescription(field.description)}</p></div>
    {!!field.children?.length && <details open={expanded}><summary><ChevronRight size={13} aria-hidden="true" />子字段<span>{field.children.length}</span></summary><FieldList fields={field.children} expanded={expanded} request={request} /></details>}
  </div>)}{fields.length > 8 && <Button variant="ghost" className={styles.more} onClick={() => setShowAll(value => !value)} aria-expanded={showAll}>{showAll ? '收起字段' : `展开其余 ${fields.length - 8} 个字段`}</Button>}</div>
}

function CodeBlock({ title, language, text }: { title: string; language: string; text: string }) {
  const [expanded, setExpanded] = useState(false)
  const long = text.split('\n').length > 18
  return <section className={styles.codeBlock} aria-label={title}><div className={styles.codeHeading}><h3>{title}<span>{language}</span></h3><CopyButton text={text} label={`复制${title}`} iconOnly /></div><pre className={!expanded && long ? styles.collapsedCode : undefined} tabIndex={0} aria-label={title}><code>{text}</code></pre>{long && <Button variant="ghost" className={styles.more} onClick={() => setExpanded(value => !value)} aria-expanded={expanded}>{expanded ? '收起示例' : '展开完整示例'}</Button>}</section>
}

function CopyButton({ text, label, iconOnly = false }: { text: string; label: string; iconOnly?: boolean }) {
  const [status, setStatus] = useState('')
  useEffect(() => { if (!status) return; const timer = window.setTimeout(() => setStatus(''), 2500); return () => window.clearTimeout(timer) }, [status])
  async function copy() { try { await navigator.clipboard.writeText(text); setStatus('已复制') } catch { setStatus('复制失败，请手动选择') } }
  return <span className={styles.copyWrap}><Button variant="ghost" size={iconOnly ? 'icon' : 'sm'} onClick={() => void copy()} aria-label={label} title={label}>{status === '已复制' ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}{!iconOnly && label}</Button><span className={styles.copyStatus} role="status">{status}</span></span>
}
