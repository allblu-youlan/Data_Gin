import { useEffect, useId, useRef, useState } from 'react'
import { Dialog, Tabs } from 'radix-ui'
import { ArrowLeft, ArrowRight, Check, ChevronRight, Copy, Menu, Search, X } from 'lucide-react'
import { Button } from './components/ui/button'
import { Input } from './components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './components/ui/table'
import ApiDocsExportMenu from './ApiDocsExportMenu'
import ApiDocsFields from './ApiDocsFields'
import { apiURL } from './apiURL'
import { buildCurl, filterApiGroups, parseApiDocument, type ApiDocument, type ApiGroup, type ApiOperation } from './apiDocs'
import { apiOperationLabel, apiOperationTitle } from './apiDocsPresentation'
import styles from './ApiDocsPage.module.css'

const baseURL = import.meta.env.VITE_API_BASE_URL ?? ''
const authID = 'authentication'
const readHash = () => decodeURIComponent(window.location.hash.slice(1))
const operationLink = (id: string, section = '') => '#' + encodeURIComponent(id) + (section ? '/' + section : '')
const sections = [['request', '请求参数'], ['response', '响应字段'], ['examples', '调用示例'], ['errors', '错误说明']]

export default function ApiDocsPage() {
  const [document, setDocument] = useState<ApiDocument | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [query, setQuery] = useState(() => new URLSearchParams(window.location.search).get('permission') ?? '')
  const [route, setRoute] = useState(readHash)
  const [menuOpen, setMenuOpen] = useState(false)
  const [selected, section] = route.split('/')
  const mainRef = useRef<HTMLElement>(null)
  const directoryRef = useRef<HTMLDivElement>(null)
  const launcherRef = useRef<HTMLButtonElement | null>(null)
  const navigateFromDirectory = useRef(false)
  const lastOperation = useRef('')
  const requestURL = new URL(baseURL || '/', window.location.origin).href

  useEffect(() => {
    const onHash = () => { setRoute(readHash()); setMenuOpen(false) }
    window.addEventListener('hashchange', onHash)
    const desktop = window.matchMedia('(min-width: 900px)')
    const closeOnDesktop = () => { if (desktop.matches) setMenuOpen(false) }
    desktop.addEventListener('change', closeOnDesktop)
    return () => { window.removeEventListener('hashchange', onHash); desktop.removeEventListener('change', closeOnDesktop) }
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

  const groups = filterApiGroups(document?.groups ?? [], query, operation => apiOperationTitle(operation) + ' ' + apiOperationLabel(operation))
  const matches = groups.flatMap(group => group.children.flatMap(child => child.operations))
  const current = matches.find(operation => operation.id === selected) ?? matches[0]
  const isAuth = selected === authID
  const index = matches.findIndex(operation => operation.id === current?.id)
  const currentID = isAuth ? authID : current?.id

  useEffect(() => {
    if (!isAuth && current) lastOperation.current = current.id
    window.document.title = (isAuth ? '授权说明' : current ? apiOperationTitle(current) : '接口文档') + ' · allblu Docs'
  }, [isAuth, current])
  useEffect(() => {
    if (!document) return
    if (section) window.document.getElementById(section)?.scrollIntoView({ block: 'start' })
    else window.scrollTo({ top: 0 })
  }, [currentID, section, document])

  function navigate() {
    navigateFromDirectory.current = menuOpen
    setMenuOpen(false)
    if (!menuOpen) mainRef.current?.focus({ preventScroll: true })
    window.scrollTo({ top: 0 })
  }

  function openDirectory(button: HTMLButtonElement) {
    launcherRef.current = button
    navigateFromDirectory.current = false
    setMenuOpen(true)
  }

  const directory = <Directory groups={groups} currentID={isAuth ? undefined : current?.id} query={query} onNavigate={navigate} onClear={() => setQuery('')} />
  return <Dialog.Root open={menuOpen} onOpenChange={setMenuOpen}>
    <div className={styles.page}>
      <a className={styles.skip} href="#api-detail" onClick={event => { event.preventDefault(); mainRef.current?.focus() }}>跳到文档内容</a>
      <header className={styles.header}>
        <a href="?view=api-docs" className={styles.brand} aria-label="allblu 接口文档"><strong>allblu</strong><span>Docs</span></a>
        <nav aria-label="文档导航" className={styles.topNav}>
          <a href={operationLink(isAuth ? lastOperation.current || current?.id || '' : current?.id || '')} aria-current={!isAuth ? 'page' : undefined} onClick={() => { setQuery(''); navigate() }}>接口文档</a>
          <a href={operationLink(authID)} aria-current={isAuth ? 'page' : undefined} onClick={navigate}>授权说明</a>
        </nav>
        <div className={styles.headerActions}>
          <div className={styles.desktopSearch}><DocsSearch query={query} onChange={setQuery} /></div>
          {document && <span className={styles.version}>v{document.version}</span>}
          <ApiDocsExportMenu document={document} currentOperationId={isAuth ? undefined : current?.id} baseURL={requestURL} disabled={Boolean(error)} />
          <Button variant="ghost" size="icon" className={styles.mobileAction} aria-label="搜索接口或字段" aria-haspopup="dialog" aria-controls="api-directory-mobile" onClick={event => openDirectory(event.currentTarget)}><Search aria-hidden="true" /></Button>
          <Button variant="ghost" className={styles.mobileAction} aria-label="打开接口目录" aria-haspopup="dialog" aria-expanded={menuOpen} aria-controls="api-directory-mobile" onClick={event => openDirectory(event.currentTarget)}><Menu aria-hidden="true" /><span className={styles.menuLabel}>目录</span></Button>
        </div>
      </header>
      <div className={styles.layout}>
        <aside className={styles.sidebar} aria-label="接口目录">{directory}</aside>
        <main id="api-detail" ref={mainRef} className={styles.main} tabIndex={-1}>
          {error ? <div className={styles.feedback} role="alert"><h1>文档暂时不可用</h1><p>{error}</p><Button variant="outline" onClick={() => setAttempt(value => value + 1)}>重新加载</Button></div>
            : !document ? <div className={styles.feedback} role="status">正在加载接口文档…</div>
            : isAuth ? <Authorization document={document} onNavigate={() => { setQuery(''); navigate() }} />
            : current ? <>
              <OperationDetail key={current.id} operation={current} requestURL={requestURL} query={query} />
              <nav className={styles.pagination} aria-label="相邻接口">
                {index > 0 ? <a href={operationLink(matches[index - 1].id)} onClick={navigate}><ArrowLeft size={16} aria-hidden="true" /><span><small>上一个接口</small>{apiOperationLabel(matches[index - 1])}</span></a> : <span />}
                {index < matches.length - 1 && <a href={operationLink(matches[index + 1].id)} onClick={navigate}><span><small>下一个接口</small>{apiOperationLabel(matches[index + 1])}</span><ArrowRight size={16} aria-hidden="true" /></a>}
              </nav>
            </> : query.trim() ? <div className={styles.feedback}><h1>未找到匹配接口</h1><p>试试接口名称、路径或字段名称。</p><Button variant="outline" onClick={() => setQuery('')}>清除搜索</Button></div>
              : <div className={styles.feedback}>当前没有已发布的接口文档。</div>}
        </main>
      </div>
    </div>
    <Dialog.Portal>
      <Dialog.Overlay className={styles.directoryOverlay} />
      <Dialog.Content id="api-directory-mobile" ref={directoryRef} className={styles.mobileDirectory}
        onOpenAutoFocus={event => { event.preventDefault(); directoryRef.current?.querySelector('input')?.focus() }}
        onCloseAutoFocus={event => {
          event.preventDefault()
          if (navigateFromDirectory.current) mainRef.current?.focus({ preventScroll: true })
          else launcherRef.current?.focus()
          navigateFromDirectory.current = false
        }}>
        <div className={styles.drawerHeading}><Dialog.Title>接口目录</Dialog.Title><Dialog.Close asChild><Button variant="ghost" size="icon" aria-label="关闭接口目录"><X aria-hidden="true" /></Button></Dialog.Close></div>
        <Dialog.Description className={styles.srOnly}>搜索并选择要查看的接口。</Dialog.Description>
        <DocsSearch query={query} onChange={setQuery} />
        {directory}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
}

function DocsSearch({ query, onChange }: { query: string; onChange: (value: string) => void }) {
  return <div className={styles.search}><Search size={17} aria-hidden="true" /><Input type="search" name="api-search" autoComplete="off" spellCheck={false} value={query} onChange={event => onChange(event.currentTarget.value)} placeholder="搜索接口或字段" aria-label="搜索接口、路径、字段或权限码" /></div>
}

function Directory({ groups, currentID, query, onNavigate, onClear }: { groups: ApiGroup[]; currentID?: string; query: string; onNavigate: () => void; onClear: () => void }) {
  const count = groups.reduce((total, group) => total + group.children.reduce((sum, child) => sum + child.operations.length, 0), 0)
  return <>
    <div className={styles.directoryHeading}>接口目录<span className={styles.srOnly} role="status">{count} 个接口</span></div>
    <nav aria-label="业务接口">{groups.map(group => {
      const operations = group.children.flatMap(child => child.operations)
      return <details className={styles.navGroup} key={group.code} open={operations.some(operation => operation.id === currentID) || Boolean(query.trim())}>
        <summary>{group.name}<ChevronRight size={15} aria-hidden="true" /></summary>
        <ul>{operations.map(operation => <li key={operation.id}><a href={operationLink(operation.id)} title={operation.title} aria-current={currentID === operation.id ? 'page' : undefined} onClick={onNavigate}>{operation.title}</a></li>)}</ul>
      </details>
    })}</nav>
    {!count && query.trim() && <div className={styles.noMatches}><p>没有匹配的接口</p><Button variant="ghost" onClick={onClear}>清除搜索</Button></div>}
  </>
}

function OperationDetail({ operation: op, requestURL, query }: { operation: ApiOperation; requestURL: string; query: string }) {
  const fields = op.requestFields.filter(field => !(field.location === 'header' && ['token', 'content-type'].includes(field.name.toLowerCase())))
  const [activeSection, setActiveSection] = useState('request')
  useEffect(() => {
    const onScroll = () => {
      const firstSection = window.document.getElementById('request')
      if (!firstSection) return
      const offset = parseFloat(window.getComputedStyle(firstSection).scrollMarginTop) + 2
      let active = 'request'
      for (const [id] of sections) if ((window.document.getElementById(id)?.getBoundingClientRect().top ?? Infinity) <= offset) active = id
      setActiveSection(active)
    }
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    window.addEventListener('resize', onScroll)
    return () => { window.removeEventListener('scroll', onScroll); window.removeEventListener('resize', onScroll) }
  }, [op.id])
  return <article>
    <header className={styles.operationHeader}>
      <h1>{apiOperationTitle(op)}</h1>
      <div className={styles.endpoint}>
        <span className={styles.method} data-method={op.method}>{op.method}</span><code>{op.path}</code>
        <CopyButton text={op.path} label="复制路径" />
      </div>
    </header>
    <nav className={styles.sectionNav} aria-label="本页内容">{sections.map(([id, label]) => <a key={id} href={operationLink(op.id, id)} aria-current={activeSection === id ? 'location' : undefined} onClick={() => { setActiveSection(id); window.document.getElementById(id)?.scrollIntoView({ block: 'start' }) }}>{label}</a>)}</nav>
    <ApiDocsFields key={'request-' + query} id="request" title="请求参数" fields={fields} request query={query} />
    <ApiDocsFields key={'response-' + query} id="response" title="响应字段" fields={op.responseFields} query={query} />
    <section id="examples" className={styles.section}><h2>调用示例</h2><Examples operation={op} requestURL={requestURL} /></section>
    <section id="errors" className={styles.section}>
      <h2>错误说明</h2>
      {op.errors.length ? <Table className={styles.errorTable}><TableHeader><TableRow><TableHead>HTTP</TableHead><TableHead>业务码</TableHead><TableHead>含义与处理</TableHead></TableRow></TableHeader><TableBody>{op.errors.map((error, i) => <TableRow key={error.status + '-' + error.code + '-' + i}><TableCell>{error.status}</TableCell><TableCell><code>{error.code}</code></TableCell><TableCell>{error.description}</TableCell></TableRow>)}</TableBody></Table> : <p className={styles.muted}>未提供额外错误说明。</p>}
    </section>
    {(!!op.notes?.length || op.description !== op.title) && <section className={styles.section}><h2>补充说明</h2><div className={styles.notes}>
      {op.description && op.description !== op.title && <details><summary>接口说明</summary><p>{op.description}</p></details>}
      {op.notes?.map((note, i) => <details key={i + '-' + note.title}><summary>{note.title}</summary><p>{note.body}</p></details>)}
    </div></section>}
  </article>
}

function Examples({ operation, requestURL }: { operation: ApiOperation; requestURL: string }) {
  const examples = [
    { id: 'curl', label: 'cURL', text: buildCurl(operation, requestURL) },
    ...(operation.method.toUpperCase() !== 'GET' && operation.requestExample != null ? [{ id: 'request-json', label: '请求 JSON', text: JSON.stringify(operation.requestExample, null, 2) }] : []),
    { id: 'response-json', label: '响应 JSON', text: JSON.stringify(operation.responseExample ?? {}, null, 2) },
  ]
  const [selected, setSelected] = useState('curl')
  const current = examples.find(example => example.id === selected) ?? examples[0]
  return <Tabs.Root className={styles.codeBlock} value={selected} onValueChange={setSelected}>
    <div className={styles.codeHeading}>
      <Tabs.List aria-label="示例类型" className={styles.codeTabs}>{examples.map(example => <Tabs.Trigger key={example.id} value={example.id}>{example.label}</Tabs.Trigger>)}</Tabs.List>
      <CopyButton key={current.id} text={current.text} label="复制示例" />
    </div>
    {examples.map(example => <Tabs.Content key={example.id} value={example.id} className={styles.codePanel}><CodeText text={example.text} label={example.label + '示例'} /></Tabs.Content>)}
  </Tabs.Root>
}

function CodeText({ text, label }: { text: string; label: string }) {
  const codeID = useId()
  const [expanded, setExpanded] = useState(false)
  const long = text.split('\n').length > 18
  return <><pre id={codeID} className={!expanded && long ? styles.collapsedCode : undefined} tabIndex={0} aria-label={label}><code>{text}</code></pre>{long && <Button variant="ghost" className={styles.more} onClick={() => setExpanded(value => !value)} aria-expanded={expanded} aria-controls={codeID}>{expanded ? '收起示例' : '展开完整示例'}</Button>}</>
}

function Authorization({ document, onNavigate }: { document: ApiDocument; onNavigate: () => void }) {
  const headers = 'token: dg_open_REPLACE_WITH_YOUR_TOKEN\nContent-Type: application/json'
  return <article className={styles.authorization}>
    <h1>授权说明</h1>
    <section className={styles.section}><h2>获取凭证</h2><p>联系管理员创建开放接口账号，获取以 <code>dg_open_</code> 开头的完整凭证，并开通所需接口权限与数据范围。</p></section>
    <section className={styles.section}><h2>添加请求头</h2><p>将凭证放入 <code>token</code> 请求头，不加 <code>Bearer</code> 前缀。</p><div className={styles.codeBlock}><div className={styles.codeHeading}><h3>通用请求头</h3><CopyButton text={headers} label="复制请求头" /></div><CodeText text={headers} label="通用请求头" /></div></section>
    <section className={styles.section}><h2>接口权限</h2><p>一级权限覆盖所属二级接口；商场、店铺等数据范围仍按账号配置生效。</p>
      {document.groups.map(group => <div key={group.code} className={styles.permissionGroup}><h3>{group.name}<code>{group.code}</code></h3>{group.children.filter(child => child.operations.length).map(child => <div key={child.code} className={styles.permissionRow}><span>{child.name}</span><code>{child.code}</code><a href={operationLink(child.operations[0].id)} onClick={onNavigate}>查看接口<ArrowRight size={14} aria-hidden="true" /></a></div>)}</div>)}
    </section>
    <section className={styles.section}><h2>授权失败</h2><p><code>401</code>：检查凭证是否完整、有效。<br /><code>403</code>：检查接口权限及账号数据范围。</p></section>
  </article>
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [status, setStatus] = useState('')
  useEffect(() => { if (!status) return; const timer = window.setTimeout(() => setStatus(''), 2500); return () => window.clearTimeout(timer) }, [status])
  async function copy() { try { await navigator.clipboard.writeText(text); setStatus('已复制') } catch { setStatus('复制失败，请手动选择') } }
  return <span className={styles.copyWrap}><Button variant="ghost" size="sm" onClick={() => void copy()} aria-label={label}>{status === '已复制' ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}{label}</Button><span className={styles.copyStatus} role="status">{status}</span></span>
}
