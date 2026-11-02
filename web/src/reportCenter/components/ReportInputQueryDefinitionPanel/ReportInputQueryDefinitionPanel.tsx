import { formatShanghaiDateTime } from '../../../dateTime.js'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Braces, FlaskConical, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { Button, DataTable, Dialog, Drawer, FeedbackState, StatusTag } from '../../../ui'
import {
	createReportInputQueryDefinition,
	deleteReportInputQueryDefinition,
	getReportInputQueryDefinitions,
	testReportInputQueryDefinition,
	updateReportInputQueryDefinition,
	type ReportCenterClient,
} from '../../api'
import type { ReportInputQueryDefinition, ReportInputQueryDefinitionInput, ReportInputQueryTestResult } from '../../types'
import { isReportInputQueryName, reportInputQueryNamePatternSource } from '../../inputQueryName'
import styles from './ReportInputQueryDefinitionPanel.module.css'

type EditorState = { open: false } | { open: true; definition: ReportInputQueryDefinition | null }

export function ReportInputQueryDefinitionPanel({ client }: { client: ReportCenterClient }) {
	const [items, setItems] = useState<ReportInputQueryDefinition[]>([])
	const [state, setState] = useState({ loading: true, error: '', notice: '' })
	const [editor, setEditor] = useState<EditorState>({ open: false })
	const [testingId, setTestingId] = useState(0)
	const [pendingDelete, setPendingDelete] = useState<ReportInputQueryDefinition | null>(null)
	const [deleting, setDeleting] = useState(false)
	const deleteButtonRef = useRef<HTMLButtonElement>(null)

	const load = useCallback(async () => {
		setState((current) => ({ ...current, loading: true, error: '' }))
		const response = await getReportInputQueryDefinitions(client)
		if (!response.ok) {
			setState({ loading: false, error: response.error, notice: '' })
			return
		}
		setItems(response.data)
		setState((current) => ({ ...current, loading: false, error: '' }))
	}, [client])

	useEffect(() => { void load() }, [load])

	async function testSaved(definition: ReportInputQueryDefinition) {
		setTestingId(definition.id)
		setState((current) => ({ ...current, notice: '', error: '' }))
		const response = await testReportInputQueryDefinition(client, definition.id, definition.selectSql)
		setTestingId(0)
		if (!response.ok) {
			setState((current) => ({ ...current, error: response.error }))
			return
		}
		setState((current) => ({ ...current, notice: testMessage(response.data) }))
		await load()
	}

	async function confirmDelete() {
		if (!pendingDelete || deleting) return
		setDeleting(true)
		const response = await deleteReportInputQueryDefinition(client, pendingDelete.id, pendingDelete.lockVersion)
		setDeleting(false)
		if (!response.ok) {
			setState((current) => ({ ...current, error: response.error }))
			setPendingDelete(null)
			return
		}
		setPendingDelete(null)
		setState((current) => ({ ...current, notice: '查询定义已删除。', error: '' }))
		await load()
	}

	return <div className={styles.root}>
		<div className={styles.toolbar}>
			<div><strong>输入选项查询</strong><span>维护查询名称和 SELECT。所有查询都使用环境变量中的默认 Oracle 连接，并且必须返回 id、name 两列。</span></div>
			<button type="button" onClick={() => setEditor({ open: true, definition: null })}><Plus aria-hidden="true" />新增查询</button>
		</div>
		<div className={styles.guide}><strong>使用步骤</strong><span>① 新增并测试查询；② 编辑报表筛选字段；③ 选择 Oracle 查询名称；④ 保存并发布报表。</span></div>
		{state.notice ? <p className={styles.notice} role="status">{state.notice}</p> : null}
		{state.loading ? <FeedbackState kind="loading" title="正在读取输入选项查询" /> : null}
		{state.error ? <FeedbackState kind="error" title="输入选项查询不可用" description={state.error} action={<button type="button" onClick={() => void load()}>重试</button>} /> : null}
		{!state.loading && !state.error && items.length === 0 ? <FeedbackState kind="empty" title="暂无输入选项查询" description="先新增款号、款色等查询，再到报表筛选条件中绑定。" /> : null}
		{items.length ? <DataTable density="compact" minWidth={900} scrollLabel="输入选项查询列表">
			<thead><tr><th scope="col">查询名称</th><th scope="col">SELECT</th><th scope="col">状态</th><th scope="col">最近测试</th><th scope="col">操作</th></tr></thead>
			<tbody>{items.map((item) => <tr key={item.id}>
				<td><span className={styles.identity}><Braces aria-hidden="true" /><code>{item.name}</code></span></td>
				<td><code className={styles.sql}>{item.selectSql}</code></td>
				<td><StatusTag tone={item.enabled ? 'success' : 'neutral'}>{item.enabled ? '启用' : '停用'}</StatusTag></td>
				<td><TestStatus definition={item} /></td>
				<td><span className={styles.actions}>
					<button type="button" onClick={() => setEditor({ open: true, definition: item })}><Pencil aria-hidden="true" />编辑</button>
					<button type="button" disabled={testingId !== 0} onClick={() => void testSaved(item)}><RefreshCw aria-hidden="true" />{testingId === item.id ? '测试中…' : '测试'}</button>
					<button ref={pendingDelete?.id === item.id ? deleteButtonRef : undefined} className={styles.delete} type="button" onClick={() => setPendingDelete(item)}><Trash2 aria-hidden="true" />删除</button>
				</span></td>
			</tr>)}</tbody>
		</DataTable> : null}
		{editor.open ? <ReportInputQueryDefinitionDrawer client={client} definition={editor.definition} onClose={() => setEditor({ open: false })} onSaved={() => { setEditor({ open: false }); void load() }} /> : null}
		<Dialog open={Boolean(pendingDelete)} role="alertdialog" title="删除输入选项查询" description={pendingDelete ? `确认删除“${pendingDelete.name}”？` : undefined} closeDisabled={deleting} returnFocus={deleteButtonRef.current} onClose={() => setPendingDelete(null)} footer={<><button type="button" disabled={deleting} onClick={() => setPendingDelete(null)}>取消</button><Button variant="danger" disabled={deleting} onClick={() => void confirmDelete()}>{deleting ? '删除中…' : '确认删除'}</Button></>}>
			<p className={styles.dangerNotice}>已被当前报表草稿或发布版本使用的查询不能删除。</p>
		</Dialog>
	</div>
}

function ReportInputQueryDefinitionDrawer({ client, definition, onClose, onSaved }: { client: ReportCenterClient; definition: ReportInputQueryDefinition | null; onClose: () => void; onSaved: () => void }) {
	const [input, setInput] = useState<ReportInputQueryDefinitionInput>(() => definitionInput(definition))
	const [state, setState] = useState<{ busy: boolean; error: string; test: ReportInputQueryTestResult | null }>({ busy: false, error: '', test: null })
	const set = useCallback(<K extends keyof ReportInputQueryDefinitionInput>(key: K, value: ReportInputQueryDefinitionInput[K]) => setInput((current) => ({ ...current, [key]: value })), [])

	async function save() {
		const error = validateDefinition(input)
		if (error) { setState({ busy: false, error, test: state.test }); return }
		setState((current) => ({ ...current, busy: true, error: '' }))
		const response = definition
			? await updateReportInputQueryDefinition(client, definition.id, { ...input, expectedLockVersion: definition.lockVersion })
			: await createReportInputQueryDefinition(client, input)
		if (!response.ok) { setState((current) => ({ ...current, busy: false, error: response.error })); return }
		onSaved()
	}

	async function testQuery() {
		const error = validateDefinition(input)
		if (error) { setState({ busy: false, error, test: null }); return }
		setState({ busy: true, error: '', test: null })
		const unchanged = definition && definition.selectSql.trim() === input.selectSql.trim()
		const response = await testReportInputQueryDefinition(client, unchanged ? definition.id : null, input.selectSql)
		if (!response.ok) { setState({ busy: false, error: response.error, test: null }); return }
		setState({ busy: false, error: response.data.status === 'FAILED' ? response.data.message : '', test: response.data })
	}

	const footer = <><button type="button" disabled={state.busy} onClick={onClose}>取消</button><button type="button" disabled={state.busy} onClick={() => void testQuery()}><FlaskConical aria-hidden="true" />{state.busy ? '处理中…' : '测试查询'}</button><Button variant="primary" disabled={state.busy} onClick={() => void save()}>{state.busy ? '处理中…' : '保存查询'}</Button></>
	return <Drawer open title={definition ? '编辑输入选项查询' : '新增输入选项查询'} description="查询保存在 MySQL，但始终通过环境变量配置的默认 Oracle 执行。SELECT 必须输出 id 和 name；不要填写分号或 SQL 注释。" size="wide" closeDisabled={state.busy} onClose={onClose} footer={footer}>
		<form className={styles.form} onSubmit={(event) => { event.preventDefault(); void save() }}>
			<label className={styles.field}><span>查询名称</span><input required disabled={state.busy} className={styles.mono} maxLength={64} pattern={reportInputQueryNamePatternSource} value={input.name} onChange={(event) => set('name', event.currentTarget.value)} placeholder="款号" /><small>报表字段通过这个名称绑定，例如“款号”“款色”或 product_color。</small></label>
			<label className={styles.sqlField}><span>SELECT 语句</span><textarea required disabled={state.busy} maxLength={65536} rows={9} value={input.selectSql} onChange={(event) => set('selectSql', event.currentTarget.value)} placeholder="SELECT a.id AS id, a.name AS name FROM BOSNDS3.M_PRODUCT@LINK_TO_BOJUN a" /><small>必须只返回 id、name 两列。运行时一次读取最多 500 条并缓存到当前浏览器会话，输入名称或 ID 时直接在本地模糊搜索。</small></label>
			<label className={styles.switch}><input disabled={state.busy} type="checkbox" checked={input.enabled} onChange={(event) => set('enabled', event.currentTarget.checked)} /><span>启用并允许报表字段绑定</span></label>
		</form>
		{state.test ? <QueryPreview result={state.test} /> : null}
		{state.error ? <p className={styles.error} role="alert">{state.error}</p> : null}
	</Drawer>
}

function QueryPreview({ result }: { result: ReportInputQueryTestResult }) {
	return <div className={styles.preview} role="status"><strong>{testMessage(result)}</strong>{result.items.length ? <ul>{result.items.map((item) => <li key={item.id}><code>{item.id}</code><span>{item.name}</span></li>)}</ul> : <span>没有返回选项。</span>}</div>
}

function TestStatus({ definition }: { definition: ReportInputQueryDefinition }) {
	if (definition.lastTestStatus === 'NOT_TESTED') return <span className={styles.muted}>尚未测试</span>
	const success = definition.lastTestStatus === 'SUCCESS'
	return <span className={styles.testStatus}><StatusTag tone={success ? 'success' : 'danger'}>{success ? '通过' : '失败'}</StatusTag><small>{formatDate(definition.lastTestedAt)}{!success && definition.lastTestError ? ` · ${definition.lastTestError}` : ''}</small></span>
}

function definitionInput(definition: ReportInputQueryDefinition | null): ReportInputQueryDefinitionInput {
	return definition ? { name: definition.name, selectSql: definition.selectSql, enabled: definition.enabled } : { name: '', selectSql: '', enabled: true }
}

function validateDefinition(input: ReportInputQueryDefinitionInput) {
	const name = input.name.trim()
	const sql = input.selectSql.trim()
	if (!isReportInputQueryName(name)) return '查询名称必须以文字开头，只能包含文字、数字、下划线或连字符。'
	if (!/^select\s/i.test(sql) || sql.length > 65536 || sql.includes(';') || sql.includes('--') || sql.includes('/*') || sql.includes('*/')) return '请输入单条无分号、无注释的 SELECT 语句。'
	return ''
}

function testMessage(result: ReportInputQueryTestResult) {
	return result.status === 'SUCCESS' ? `查询通过，返回 ${result.rowCount} 条（${result.latencyMs} ms）。` : result.message || '查询测试失败。'
}

function formatDate(value: string | null) { return formatShanghaiDateTime(value, '-') }
