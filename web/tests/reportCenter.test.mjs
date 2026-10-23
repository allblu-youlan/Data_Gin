import assert from 'node:assert/strict'
import { isBojunColumnAllowed } from '../.test-dist/reportCenter/bojunQueryConfig.js'
import { isSimpleRuntimeWhere, normalizeRuntimeQueryInputs, readRuntimeFilter, runtimeDateInput, runtimeDateValue, updateRuntimeFilter, validateRuntimeQueryInputs } from '../.test-dist/reportCenter/components/ReportQueryDesigner/runtimeFilters.js'
import { readdirSync, readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

import { activateReportVersion, cancelReportRun, createReportExport, createReportInputQueryDefinition, createReportRun, deleteReportDraft, getReportAudits, getReportDownloads, getReportInputOptions, getReportInputQueries, getReportInputQueryDefinitions, getReportProcedureSignature, getReportProcedures, getReportResultTableSchema, getReportResultTables, getReportRun, getReportVersion, isRetryableReportPollFailure, parsePublication, parseReportAuditPage, parseReportCatalogPage, parseReportDatasource, parseReportDatasources, parseReportDatasourceTest, parseReportDraft, parseReportExport, parseReportExportPage, parseReportInputQueryDefinitions, parseReportInputQueryTestResult, parseReportProcedurePage, parseReportProcedureSignature, parseReportResultPage, parseReportResultTablePage, parseReportResultTableSchema, parseReportRun, parseReportRunContract, parseReportVersionDetail, parseReportVersionDiff, parseReportVersionPage, reportPollRetryDelay, saveAndPublishReportDraft, saveReportDraft, testReportDatasourceConnection, testReportInputQueryDefinition } from '../.test-dist/reportCenter/api.js'
import { applyExcelMapping, buildReportConditions, excelMappingFromColumns, initialReportConditionValues, moveReportInputField, orderedReportInputEntries, parseExcelMappingDocument, parseReportInputSchemaDocument, parseReportInputSchemaText, reconcileReportColumnsWithResultSchema, renameExcelMappingField, reportColumnsFromResultSchema } from '../.test-dist/reportCenter/refCursorConfig.js'
import { reportParameterControls, reportParameterFlagDisabled, updateReportParameterFlag, updateReportParameterLogicalType } from '../.test-dist/reportCenter/parameterConfig.js'
import { buildNewReportRunState, canBindReportExport, canRetryReportExportBinding, canStartNewReportRun, initialReportParameterValues, isReportExportSettled } from '../.test-dist/reportCenter/queryParameters.js'
import { createLatestRequestGuard } from '../.test-dist/reportCenter/components/ReportVersionDrawer/requestGuard.js'
import { normalizeDatasourceCode, validateDatasourceConnection, validateDatasourceSave } from '../.test-dist/reportCenter/datasourceValidation.js'
import { mergeReportInputOptions, reportInputOptionMatches, reportInputSelectionKeys, reportInputSelectionValue } from '../.test-dist/reportCenter/inputOptions.js'
import { isReportInputQueryName } from '../.test-dist/reportCenter/inputQueryName.js'
import { previewReportTimeDefault, reportTimeDefaultOptions } from '../.test-dist/reportCenter/timeDefaults.js'
import { parseReportQueryConditionGroup } from '../.test-dist/reportCenter/bojunQueryConfig.js'
import { computedReportValuePreview, reportTimeOperator } from '../.test-dist/reportCenter/components/ReportQueryDesigner/computedValues.js'

test('numeric and date month ranges stay dynamic through query normalization and configuration', () => {
  const fields = [
    { aliasId: 'base', columnId: '1', label: '数字日期', logicalType: 'integer', operators: ['EQ', 'GTE', 'LTE', 'BETWEEN'] },
    { aliasId: 'base', columnId: '2', label: '日期', logicalType: 'date', operators: ['EQ', 'GTE', 'LTE', 'BETWEEN'] },
  ]
  let value = {}
  for (const field of fields) value = updateRuntimeFilter(value, field, 'BETWEEN', undefined, { kind: 'TIME_RANGE', code: 'CURRENT_MONTH' })
  assert.equal(isSimpleRuntimeWhere({ allowedFilters: fields }, value), true)
  assert.equal(validateRuntimeQueryInputs({ allowedFilters: fields }, value), '')
  assert.deepEqual(normalizeRuntimeQueryInputs(value), value)
  assert.deepEqual(parseReportQueryConditionGroup(value.where), value.where)
  const now = new Date('2026-10-23T03:00:00Z')
  assert.match(computedReportValuePreview({ kind: 'TIME_RANGE', code: 'CURRENT_MONTH' }, 'integer', now), /20261001 至 20261031.*含首尾/)
  assert.match(computedReportValuePreview({ kind: 'TIME_RANGE', code: 'CURRENT_MONTH' }, 'date', now), /2026-10-01 至 2026-10-31.*含末日全天/)
  const single = updateRuntimeFilter({}, fields[0], 'LTE', undefined, { kind: 'TIME', code: 'MONTH_END' })
  assert.equal(validateRuntimeQueryInputs({ allowedFilters: fields }, single), '')
  assert.equal(reportTimeOperator(['LTE']), 'LTE')
  assert.equal(reportTimeOperator(['EQ', 'GTE'], 'EQ'), 'EQ')
  assert.equal(reportTimeOperator(['BETWEEN']), undefined)
})

test('maximum filters preserve the computed source and reject ambiguous combinations', () => {
  const fields = [1, 2].map((id) => ({ aliasId: 'base', columnId: String(id), label: `字段${id}`, logicalType: 'integer', operators: ['EQ', 'GTE', 'BETWEEN'] }))
  const maximum = updateRuntimeFilter({}, fields[0], 'EQ', undefined, { kind: 'COLUMN_MAX' })
  const filtered = updateRuntimeFilter(maximum, fields[1], 'GTE', '10')
  assert.equal(isSimpleRuntimeWhere({ allowedFilters: fields }, filtered), true)
  assert.equal(validateRuntimeQueryInputs({ allowedFilters: fields }, filtered), '')
  assert.deepEqual(parseReportQueryConditionGroup(filtered.where), filtered.where)
  assert.match(validateRuntimeQueryInputs({ allowedFilters: fields }, { where: { ...maximum.where, logic: 'OR' } }), /AND/)
  assert.match(validateRuntimeQueryInputs({ allowedFilters: fields }, updateRuntimeFilter(maximum, fields[1], 'EQ', undefined, { kind: 'COLUMN_MAX' })), /最多.*一项/)
  assert.match(validateRuntimeQueryInputs({ allowedFilters: fields }, updateRuntimeFilter({}, fields[0], 'GTE', undefined, { kind: 'COLUMN_MAX' })), /等于/)
  assert.match(validateRuntimeQueryInputs({ allowedFilters: fields }, updateRuntimeFilter({}, fields[0], 'EQ', undefined, { kind: 'TIME_RANGE', code: 'CURRENT_MONTH' })), /区间/)
})

test('dynamic time defaults survive configuration and resolve at execution instead of page load', () => {
  const schema = parseReportInputSchemaDocument({
    month: { type: 'str', displayName: '月份', control: 'TEXT', required: true, defaultTime: 'CURRENT_MONTH' },
    day: { type: 'str', displayName: '日期', control: 'DATE', format: 'YYYY-MM-DD', defaultTime: 'YESTERDAY' },
    numericMonth: { type: 'number', displayName: '数字月份', defaultTime: 'PREVIOUS_MONTH' },
  })
  assert.equal(schema.month.defaultTime, 'CURRENT_MONTH')
  assert.equal(Object.hasOwnProperty.call(schema.month, 'default'), false)
  assert.deepEqual(initialReportConditionValues(schema), { month: '', day: '', numericMonth: '' })
  assert.deepEqual(buildReportConditions(schema, initialReportConditionValues(schema)), { ok: true, conditions: {} })
  assert.deepEqual(buildReportConditions(schema, { month: '202601', numericMonth: '202602', day: '2026-01-31' }), { ok: true, conditions: { month: '202601', day: '2026-01-31', numericMonth: 202602 } })
  const now = new Date('2026-10-31T16:30:45Z')
  assert.equal(previewReportTimeDefault(schema.month, now), '202611')
  assert.equal(previewReportTimeDefault(schema.day, now), '2026-10-31')
  assert.equal(previewReportTimeDefault(schema.numericMonth, now), '202610')
  assert.equal(previewReportTimeDefault({ type: 'str', control: 'TEXT', defaultTime: 'NEXT_MONTH' }, new Date('2026-12-31T00:00:00Z')), '202701')
  assert.equal(previewReportTimeDefault({ type: 'str', control: 'TEXT', defaultTime: 'PREVIOUS_MONTH' }, new Date('2026-01-31T00:00:00Z')), '202512')
  assert.equal(previewReportTimeDefault({ type: 'str', control: 'TEXT', defaultTime: 'MONTH_END' }, new Date('2028-02-15T00:00:00Z')), '20280229')
  assert.equal(previewReportTimeDefault({ type: 'str', control: 'DATETIME', format: 'YYYY-MM-DD HH:mm:ss', defaultTime: 'NOW' }, now), '2026-11-01 00:30:45')
  assert.equal(previewReportTimeDefault({ type: 'str', control: 'DATETIME', format: 'YYYYMMDDHHmmss', defaultTime: 'TODAY' }, now), '20261101000000')
  assert.equal(reportTimeDefaultOptions({ type: 'str', control: 'DATE' }).some((option) => option.value === 'CURRENT_MONTH'), false)
  assert.deepEqual(reportTimeDefaultOptions({ type: 'list[str]', control: 'TEXTAREA' }), [])
})

test('dynamic time configuration rejects unsupported types and conflicting defaults', () => {
  for (const field of [
    { type: 'bool', defaultTime: 'TODAY' },
    { type: 'str', defaultTime: 'UNKNOWN' },
    { type: 'str', defaultTime: 'TODAY', default: '20261023' },
    { type: 'str', defaultTime: 'CURRENT_MONTH', control: 'DATE', format: 'YYYYMMDD' },
    { type: 'str', defaultTime: 'TODAY', allowedValues: ['20261023'] },
  ]) assert.throws(() => parseReportInputSchemaDocument({ value: { displayName: '测试', ...field } }), /动态时间/)
})

test('functional state updates do not retain React DOM events', () => {
  const sourceRoot = new URL('../src/', import.meta.url)
  const violations = []
  for (const file of typescriptFiles(sourceRoot)) {
    const source = readFileSync(file, 'utf8')
    const sourceFile = ts.createSourceFile(file.pathname, source, ts.ScriptTarget.Latest, true, file.pathname.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
    inspect(sourceFile)

    function inspect(node) {
      if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && /^set[A-Z]/.test(node.expression.text)) {
        const updater = node.arguments[0]
        if (updater && (ts.isArrowFunction(updater) || ts.isFunctionExpression(updater))) inspectUpdater(updater.body)
      }
      ts.forEachChild(node, inspect)
    }

    function inspectUpdater(node) {
      if (ts.isPropertyAccessExpression(node) && ['currentTarget', 'target', 'nativeEvent'].includes(node.name.text)) {
        const position = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile))
        violations.push(`${file.pathname}:${position.line + 1}:${position.character + 1}`)
      }
      ts.forEachChild(node, inspectUpdater)
    }
  }
  assert.deepEqual(violations, [])
})

test('drawer keeps long parsed configuration inside its scrollable body', () => {
  const source = readFileSync(new URL('../src/ui/Drawer/Drawer.module.css', import.meta.url), 'utf8')
  const layerRule = source.match(/\.layer\s*\{([^}]*)\}/)?.[1] ?? ''
  const drawerRule = source.match(/\.drawer\s*\{([^}]*)\}/)?.[1] ?? ''
  const bodyRule = source.match(/\.body\s*\{([^}]*)\}/)?.[1] ?? ''
  assert.match(layerRule, /grid-template-rows:\s*minmax\(0,\s*1fr\)\s*;/)
  assert.match(layerRule, /overflow:\s*hidden\s*;/)
  assert.match(drawerRule, /height:\s*100%\s*;/)
  assert.match(drawerRule, /min-height:\s*0\s*;/)
  assert.match(bodyRule, /min-height:\s*0\s*;/)
  assert.match(bodyRule, /overflow-y:\s*auto\s*;/)
})

test('condition editor keeps date format out of a narrow action column', () => {
  const source = readFileSync(new URL('../src/reportCenter/components/ReportConfigDrawer/ReportInputSchemaEditor.module.css', import.meta.url), 'utf8')
  const rowRule = source.match(/\.row\s*\{([^}]*)\}/)?.[1] ?? ''
  const formatRule = source.match(/\.formatField\s*\{([^}]*)\}/)?.[1] ?? ''
  assert.match(rowRule, /grid-template-columns:\s*repeat\(4,\s*minmax\(130px,\s*1fr\)\)\s*;/)
  assert.doesNotMatch(rowRule, /38px/)
  assert.match(formatRule, /grid-column:\s*span 2\s*;/)
  assert.match(formatRule, /border-inline-start:\s*2px solid var\(--color-brand\)\s*;/)
})

test('report configuration locks its editor while save and publication are in flight', () => {
  const source = readFileSync(new URL('../src/reportCenter/components/ReportConfigDrawer/ReportConfigDrawer.tsx', import.meta.url), 'utf8')
  assert.match(source, /<fieldset[^>]*disabled=\{state\.saving \|\| draftRefreshFailed\}[^>]*aria-busy=\{state\.saving \|\| undefined\}/)
})

test('Oracle object editor searches on demand and cancels superseded catalog requests', () => {
  const source = readFileSync(new URL('../src/reportCenter/components/ReportConfigDrawer/ReportProcedureEditor.tsx', import.meta.url), 'utf8')
  assert.match(source, /catalogAbort\.current\?\.abort\(\)/)
  assert.match(source, /tableCatalogAbort\.current\?\.abort\(\)/)
  assert.match(source, /getReportProcedures\([\s\S]*?controller\.signal\)/)
  assert.match(source, /getReportResultTables\([\s\S]*?controller\.signal\)/)
  assert.doesNotMatch(source, /getReportProcedures\(client, draft\.datasourceId, \{ limit: 30 \}/)
  assert.doesNotMatch(source, /getReportResultTables\(client, draft\.datasourceId, \{ limit: 30 \}/)
})

test('report download selection cancels the superseded run-to-export chain', () => {
  const source = readFileSync(new URL('../src/reportCenter/pages/ReportQueryPage/ReportQueryPage.tsx', import.meta.url), 'utf8')
  assert.match(source, /onChange=\{\(event\) => \{ pollAbortRef\.current\?\.abort\(\); setSelectedId\(event\.currentTarget\.value\) \}\}/)
  assert.match(source, /createReportRun\([\s\S]*?controller\.signal\)/)
  assert.match(source, /createReportExport\([\s\S]*?controller\.signal\)/)
  assert.match(source, /if \(!ownsPoll\(controller\)\) return[\s\S]*?await startExport\(runId, controller\)/)
  assert.match(source, /if \(signal\?\.aborted\) return[\s\S]*?if \(!response\.ok\)/)
  assert.match(source, /if \(signal\.aborted\) \{ resolve\(\); return \}/)
  assert.match(source, /setCancelState\(\{ open: true, busy: false, error: response\.error \}\)[\s\S]*?await pollRun\(run\.id, controller\)/)
  assert.match(source, /terminalReportRunStatuses\.has\(response\.data\.status\)[\s\S]*?setOperation\(\{ busy: false, error: response\.data\.errorMessage \}\)/)
})

function typescriptFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const file = new URL(entry.name, directory)
    if (entry.isDirectory()) return typescriptFiles(new URL(`${entry.name}/`, directory))
    return /\.tsx?$/.test(entry.name) ? [file] : []
  })
}

test('parseReportCatalogPage reads the standard API envelope', () => {
  const page = parseReportCatalogPage({
    code: 200,
    msg: 'success',
    data: {
      items: [{
        id: 12,
        code: 'sales_report',
        name: '销售报表',
        category: '经营',
        datasourceId: 3,
        status: 'ACTIVE',
        lockVersion: 4,
        isOwner: false,
        updatedAt: '2026-08-12T10:00:00Z',
      }],
      hasMore: true,
      nextAfterId: 12,
    },
  })

  assert.equal(page.items.length, 1)
  assert.equal(page.items[0].name, '销售报表')
  assert.equal(page.items[0].status, 'ACTIVE')
  assert.equal(page.items[0].lockVersion, 4)
  assert.equal(page.items[0].isOwner, false)
  assert.equal(page.hasMore, true)
  assert.equal(page.nextAfterId, 12)
})

test('download catalog uses its dedicated published-only endpoint', async () => {
  const requests = []
  const client = async (path, options) => {
    requests.push({ path, options })
    return { ok: true, data: { data: { items: [], hasMore: false } } }
  }
  const result = await getReportDownloads(client, { category: ' 财务 ', search: ' 销售 ', limit: 20 })
  assert.equal(result.ok, true)
  assert.equal(requests[0].path, '/v1/report-downloads?search=%E9%94%80%E5%94%AE&category=%E8%B4%A2%E5%8A%A1&limit=20')
  assert.equal(requests[0].options.method, 'GET')

  const malformed = await getReportDownloads(async () => ({ ok: true, data: { data: { items: [], hasMore: true, nextAfterId: 0 } } }), { limit: 20 })
  assert.equal(malformed.ok, false)
})

test('input schema rejects payloads beyond the backend 64 KiB boundary', () => {
  assert.throws(
    () => parseReportInputSchemaDocument({ condition1: { type: 'VARCHAR2', displayName: '条件', example: '测'.repeat(22_000) } }),
    /64 KiB/,
  )
})

test('input schema keeps a configured query binding on scalar selectors', () => {
	const schema = parseReportInputSchemaDocument({ store: { type: 'str', displayName: '门店', control: 'SELECT', queryName: 'stores' } })
	assert.equal(schema.store.queryName, 'stores')
	assert.equal(parseReportInputSchemaDocument({ stores: { type: 'list[str]', displayName: '门店', control: 'SELECT', queryName: 'stores' } }).stores.queryName, 'stores')
	assert.equal(parseReportInputSchemaDocument({ products: { type: 'list[number]', displayName: '商品', control: 'SELECT', queryName: 'products' } }).products.queryName, 'products')
	assert.equal(parseReportInputSchemaDocument({ products: { type: 'list[str]', displayName: '商品', control: 'SELECT', queryName: '款号查询' } }).products.queryName, '款号查询')
	assert.throws(() => parseReportInputSchemaDocument({ flags: { type: 'list[bool]', displayName: '标记', control: 'SELECT', queryName: 'flags' } }), /queryName/)
	assert.throws(() => parseReportInputSchemaDocument({ store: { type: 'str', displayName: '门店', control: 'TEXT', queryName: 'stores' } }), /queryName/)
	assert.throws(() => parseReportInputSchemaDocument({ store: { type: 'str', displayName: '门店', control: 'SELECT', queryName: 'stores', allowedValues: ['S001'] } }), /allowedValues/)
})

test('report input query names accept Unicode letters consistently', () => {
	assert.equal(isReportInputQueryName('款号'), true)
	assert.equal(isReportInputQueryName('款色_2026'), true)
	assert.equal(isReportInputQueryName('1款号'), false)
	assert.equal(isReportInputQueryName('款号 查询'), false)
})

test('report input query clients load the configured option set once without remote search', async () => {
	const requests = []
	const client = async (path, options) => {
		requests.push({ path, options })
		if (path === '/v1/report-input-queries') return { ok: true, data: { data: { items: ['门店查询'] } } }
		return { ok: true, data: { data: { items: [{ id: 'S001', name: '上海店' }] } } }
	}
	assert.deepEqual(await getReportInputQueries(client), { ok: true, data: ['门店查询'] })
	assert.deepEqual(await getReportInputOptions(client, 9, 'store_id'), { ok: true, data: [{ id: 'S001', name: '上海店' }] })
	assert.equal(requests[1].path, '/v1/reports/9/input-options/store_id')
	assert.equal(requests[1].options.method, 'GET')
})

test('report input query definitions support frontend management and safe test previews', async () => {
	const definition = { id: 4, name: '款号查询', selectSql: 'SELECT id, name FROM products', enabled: true, lockVersion: 2, lastTestStatus: 'SUCCESS', lastTestError: '', lastTestedAt: '2026-08-25T08:00:00Z', createdAt: '2026-08-25T07:00:00Z', updatedAt: '2026-08-25T08:00:00Z' }
	assert.deepEqual(parseReportInputQueryDefinitions({ data: { items: [definition] } }), [definition])
	assert.deepEqual(parseReportInputQueryTestResult({ data: { status: 'SUCCESS', testedAt: '2026-08-25T08:00:00Z', latencyMs: 12, rowCount: 1, items: [{ id: 'P001', name: '款号一' }], message: 'ok' } }).items, [{ id: 'P001', name: '款号一' }])

	const requests = []
	const client = async (path, options) => {
		requests.push({ path, options })
		if (path.endsWith('tests')) return { ok: true, data: { data: { status: 'SUCCESS', testedAt: '2026-08-25T08:00:00Z', latencyMs: 12, rowCount: 1, items: [{ id: 'P001', name: '款号一' }], message: 'ok' } } }
		if (options?.method === 'POST') return { ok: true, data: { data: definition } }
		return { ok: true, data: { data: { items: [definition] } } }
	}
	assert.deepEqual(await getReportInputQueryDefinitions(client), { ok: true, data: [definition] })
	assert.equal((await createReportInputQueryDefinition(client, { name: '款号查询', selectSql: definition.selectSql, enabled: true })).ok, true)
	assert.equal((await testReportInputQueryDefinition(client, null, definition.selectSql, '款号一')).ok, true)
	assert.equal(requests[2].path, '/v1/report-input-query-definition-tests')
	assert.deepEqual(requests[2].options.body, { selectSql: definition.selectSql, name: '款号一' })
})

test('input options match names and ids locally with normalized fuzzy text', () => {
	const option = { id: ' SKU-001 ', name: '上海旗舰店' }
	assert.equal(reportInputOptionMatches(option, '旗舰'), true)
	assert.equal(reportInputOptionMatches(option, 'sku-001'), true)
	assert.equal(reportInputOptionMatches(option, '北京'), false)
})

test('local input options keep unique selections', () => {
	const selected = [{ id: 'S001', name: '上海店' }]
	const nextSearch = [{ id: 'B001', name: '北京店' }, { id: 'S001', name: '上海旗舰店' }]
	assert.deepEqual(mergeReportInputOptions(selected, nextSearch), [{ id: 'S001', name: '上海旗舰店' }, { id: 'B001', name: '北京店' }])
})

test('local input selections preserve scalar and list id shapes', () => {
	assert.deepEqual(reportInputSelectionKeys(['P001', 'P002'], true), ['P001', 'P002'])
	assert.deepEqual(reportInputSelectionValue(['1', '2'], true, true), [1, 2])
	assert.deepEqual(reportInputSelectionValue(['P001', 'P002'], true, false), ['P001', 'P002'])
	assert.equal(reportInputSelectionValue('P001', false, false), 'P001')
})

test('parseReportCatalogPage drops malformed rows and normalizes unsafe fields', () => {
  const page = parseReportCatalogPage({
    data: {
      items: [
        { id: 0, code: 'invalid', name: '无效报表' },
        { id: 9, code: 'inventory', name: '库存报表', status: 'UNKNOWN', datasourceId: '4' },
      ],
      nextAfterId: '9',
    },
  })

  assert.equal(page.items.length, 1)
  assert.equal(page.items[0].status, 'DRAFT')
  assert.equal(page.items[0].datasourceId, 4)
  assert.equal(page.items[0].isOwner, false)
  assert.equal(page.nextAfterId, 9)
})

test('parseReportCatalogPage rejects an incomplete or mismatched continuation cursor', () => {
  assert.throws(() => parseReportCatalogPage({ data: { items: [], hasMore: true, nextAfterId: 9 } }))
  assert.throws(() => parseReportCatalogPage({ data: { items: [{ id: 9, code: 'sales', name: '销售' }], hasMore: true, nextAfterId: 10 } }))
})

test('deleteReportDraft sends an optimistic DELETE and validates the deleted id', async () => {
  const requests = []
  const client = async (path, options) => {
    requests.push({ path, options })
    return { ok: true, data: { data: { id: 9 } } }
  }
  assert.deepEqual(await deleteReportDraft(client, 9, 4), { ok: true, data: { id: 9 } })
  assert.equal(requests[0].path, '/v1/reports/9?expectedLockVersion=4')
  assert.equal(requests[0].options.method, 'DELETE')

  const malformed = await deleteReportDraft(async () => ({ ok: true, data: { data: { id: 0 } } }), 9, 4)
  assert.deepEqual(malformed, { ok: false, error: '服务返回的数据格式不完整，请稍后重试。' })
})

test('parseReportRunContract keeps typed published parameters', () => {
  const contract = parseReportRunContract({ data: {
    definitionId: 9,
    versionId: 23,
    code: 'sales_report',
    name: '销售报表',
    parameters: [
      { code: 'runId', label: '运行编号', systemInjected: true, displayOrder: 1, position: 1, oracleType: 'VARCHAR2' },
      { code: 'storeCode', label: '门店', controlType: 'SELECT', logicalType: 'enum', required: true, allowedValues: ['S001', 'S002'], displayOrder: 2, position: 2, oracleType: 'VARCHAR2' },
    ],
  } })
  assert.equal(contract.versionId, 23)
  assert.equal(contract.executionMode, 'TABLE_SNAPSHOT')
  assert.equal(contract.jsonInput, false)
  assert.equal(contract.parameters[1].controlType, 'SELECT')
  assert.deepEqual(contract.parameters[1].allowedValues, ['S001', 'S002'])
})

test('parseReportRunContract exposes REF CURSOR condition schema for the query form', () => {
  const contract = parseReportRunContract({ data: {
    definitionId: 9, versionId: 24, code: 'sales_report', name: '销售报表', executionMode: 'REF_CURSOR', parameters: [],
    inputSchema: {
      store_id: { type: 'VARCHAR2', displayName: '门店', required: true, allowedValues: ['S001', 'S002'] },
      product_ids: { type: 'VARCHAR2', displayName: '商品', multiple: true, default: ['P001'] },
    },
  } })
  assert.equal(contract.executionMode, 'REF_CURSOR')
  assert.equal(contract.jsonInput, true)
  assert.equal(contract.inputSchema.store_id.displayName, '门店')
  assert.equal(contract.inputSchema.store_id.type, 'str')
  assert.equal(contract.inputSchema.product_ids.type, 'list[str]')
  assert.deepEqual(initialReportConditionValues(contract.inputSchema), { store_id: '', product_ids: ['P001'] })
  assert.deepEqual(buildReportConditions(contract.inputSchema, { store_id: 'S001', product_ids: '["P001","P002"]' }), { ok: true, conditions: { store_id: 'S001', product_ids: ['P001', 'P002'] } })
  assert.deepEqual(buildReportConditions(contract.inputSchema, { store_id: '' }), { ok: false, error: '门店 为必填筛选条件。' })
})

test('REF CURSOR numeric enum defaults keep their type for query selectors', () => {
  const schema = parseReportInputSchemaDocument({ status: { type: 'NUMBER', displayName: '状态', default: 1, allowedValues: [1, 2] } })
  assert.equal(schema.status.type, 'number')
  assert.deepEqual(initialReportConditionValues(schema), { status: 1 })
  assert.deepEqual(buildReportConditions(schema, { status: 1 }), { ok: true, conditions: { status: 1 } })
})

test('createReportRun sends conditions for JSON procedures and keeps parameters for legacy reports', async () => {
  const requests = []
  const client = async (path, options) => {
    requests.push({ path, options })
    return { ok: true, data: { data: { id: requests.length, runUuid: 'run', definitionId: 9, versionId: 24, status: 'QUEUED' } } }
  }
  await createReportRun(client, 9, { store_id: 'S001' }, 'REF_CURSOR')
  await createReportRun(client, 9, { storeCode: 'S001' }, 'TABLE_SNAPSHOT')
  await createReportRun(client, 9, { supplier_id: 'A001' }, 'TABLE_SNAPSHOT', true)
  assert.deepEqual(requests[0].options.body, { conditions: { store_id: 'S001' } })
  assert.deepEqual(requests[1].options.body, { parameters: { storeCode: 'S001' } })
  assert.deepEqual(requests[2].options.body, { conditions: { supplier_id: 'A001' } })
})

test('run and export mutations forward cancellation signals', async () => {
  const requests = []
  const signal = new AbortController().signal
  const client = async (path, options) => {
    requests.push({ path, options })
    if (path.endsWith('/cancel')) return { ok: true, data: { data: { id: 31, runUuid: 'run', definitionId: 9, versionId: 24, status: 'CANCEL_REQUESTED' } } }
    return { ok: true, data: { data: { id: 41, runId: 31, exportUuid: 'export', status: 'PENDING' } } }
  }
  await cancelReportRun(client, 31, signal)
  await createReportExport(client, 31, { filters: [], sort: [] }, signal)
  assert.equal(requests[0].options.signal, signal)
  assert.equal(requests[1].options.signal, signal)
})

test('report polling preserves transient failures and retries only recoverable responses', async () => {
  const transient = await getReportRun(async () => ({
    ok: false,
    status: 503,
    data: { message: '服务暂时不可用' },
    error: { kind: 'server', message: '服务暂时不可用', retryAfterSeconds: 7 },
  }), 31)
  assert.deepEqual(transient, {
    ok: false,
    error: '服务暂时不可用',
    kind: 'server',
    status: 503,
    retryAfterSeconds: 7,
  })
  assert.equal(isRetryableReportPollFailure(transient), true)
  assert.equal(reportPollRetryDelay(transient, 1), 7000)
  assert.equal(reportPollRetryDelay({ ok: false, error: '网络异常', kind: 'network' }, 1), 1500)
  assert.equal(reportPollRetryDelay({ ok: false, error: '网络异常', kind: 'network' }, 2), 3000)
  assert.equal(reportPollRetryDelay({ ok: false, error: '网络异常', kind: 'network' }, 4), 5000)

  assert.equal(isRetryableReportPollFailure({ ok: false, error: '网关错误', status: 502 }), true)
  assert.equal(isRetryableReportPollFailure({ ok: false, error: '请求超时', kind: 'timeout' }), true)
  assert.equal(isRetryableReportPollFailure({ ok: false, error: '请求过多', kind: 'rate_limited', status: 429 }), false)
  assert.equal(isRetryableReportPollFailure({ ok: false, error: '未登录', kind: 'unauthorized', status: 401 }), false)
  assert.equal(isRetryableReportPollFailure({ ok: false, error: '无权限', kind: 'forbidden', status: 403 }), false)
  assert.equal(isRetryableReportPollFailure({ ok: false, error: '参数错误', kind: 'client', status: 409 }), false)
  assert.equal(isRetryableReportPollFailure({ ok: false, error: '服务异常', kind: 'server', status: 500 }), false)
})

test('report parameter defaults use the same editable shapes as their controls', () => {
  const parameter = (code, logicalType, defaultValue, extra = {}) => ({
    code, logicalType, defaultValue, systemInjected: false, controlType: 'TEXT', ...extra,
  })
  const values = initialReportParameterValues([
    parameter('count', 'integer', 12),
    parameter('ratio', 'decimal', 12.5),
    parameter('options', 'json', { active: true }),
    parameter('jsonText', 'json', 'literal'),
    parameter('stores', 'multi_enum', ['S001', 2], { controlType: 'MULTI_SELECT' }),
    parameter('enabled', 'boolean', false, { controlType: 'CHECKBOX' }),
    parameter('name', 'string', ' 默认名称 '),
    parameter('emptyJson', 'json', null, { controlType: 'TEXTAREA' }),
    parameter('runId', 'string', 'ignored', { systemInjected: true }),
  ])

  assert.deepEqual(values, {
    count: '12', ratio: '12.5', options: '{"active":true}', jsonText: '"literal"', stores: ['S001', '2'],
    enabled: false, name: ' 默认名称 ', emptyJson: '',
  })
})

test('historical sensitive system parameters can be repaired without recreation', () => {
  const historical = { systemInjected: true, sensitive: true, normalizer: {}, valueSource: { source: 'RUN_ID' }, defaultValue: undefined }
  assert.equal(reportParameterFlagDisabled(historical, 'systemInjected'), false)
  assert.equal(reportParameterFlagDisabled(historical, 'sensitive'), false)
  assert.deepEqual(updateReportParameterFlag(historical, 'systemInjected', false), {
    ...historical, systemInjected: false, valueSource: {},
  })
  assert.deepEqual(updateReportParameterFlag(historical, 'sensitive', false), {
    ...historical, sensitive: false,
  })

  assert.equal(reportParameterFlagDisabled({ ...historical, systemInjected: false }, 'systemInjected'), true)
  assert.equal(reportParameterFlagDisabled({ ...historical, sensitive: false }, 'sensitive'), true)
})

test('report parameter logical types derive compatible control and collection shapes', () => {
  const parameter = {
    controlType: 'TEXTAREA', logicalType: 'string', cardinality: 'SINGLE', collectionEncoding: '',
    normalizer: { trim: true }, valueSource: { source: 'RUN_ID' },
  }
  assert.deepEqual(reportParameterControls('string'), ['TEXT', 'TEXTAREA'])
  assert.deepEqual(reportParameterControls('boolean'), ['CHECKBOX'])
  assert.deepEqual(reportParameterControls('multi_enum'), ['MULTI_SELECT'])
  assert.deepEqual(updateReportParameterLogicalType(parameter, 'multi_enum'), {
    ...parameter, logicalType: 'multi_enum', controlType: 'MULTI_SELECT', cardinality: 'MULTIPLE',
    collectionEncoding: 'JSON_CLOB', valueSource: {},
  })
  assert.deepEqual(updateReportParameterLogicalType(parameter, 'json'), {
    ...parameter, logicalType: 'json', controlType: 'TEXTAREA', cardinality: 'SINGLE',
    collectionEncoding: '', normalizer: {}, valueSource: {},
  })
})

test('new report runs are allowed only after run and export processing finish', () => {
  const runStatuses = ['QUEUED', 'RUNNING', 'CANCEL_REQUESTED', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'UNKNOWN', 'RECONCILING', 'EXPORTING', 'EXPORTED', 'RESULT_PURGING', 'RESULT_PURGED', 'SUPERSEDED']
  const exportStatuses = [null, 'PENDING', 'RUNNING', 'READY', 'FAILED', 'CANCELLED', 'EXPIRED']
  const terminalRuns = new Set(['SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPORTED', 'RESULT_PURGED', 'SUPERSEDED'])
  const terminalExports = new Set([null, 'FAILED', 'CANCELLED', 'EXPIRED'])
  for (const runStatus of runStatuses) {
    for (const exportStatus of exportStatuses) {
      const reportExport = exportStatus === null ? null : { status: exportStatus, purgedAt: null }
      assert.equal(canStartNewReportRun(runStatus, reportExport, false), terminalRuns.has(runStatus) && terminalExports.has(exportStatus), `${runStatus}/${exportStatus}`)
      assert.equal(canStartNewReportRun(runStatus, reportExport, true), false, `${runStatus}/${exportStatus}/busy`)
    }
  }
  assert.equal(canStartNewReportRun('SUCCEEDED', { status: 'READY', purgedAt: null }, false), false)
  assert.equal(canStartNewReportRun('SUCCEEDED', { status: 'READY', purgedAt: '2026-09-04T08:30:00Z' }, false), true)
})

test('completed runs can bind the user export after Oracle results were cleaned', () => {
  assert.equal(canBindReportExport({ status: 'SUCCEEDED', resultAvailable: true }), true)
  assert.equal(canBindReportExport({ status: 'EXPORTING', resultAvailable: false }), true)
  assert.equal(canBindReportExport({ status: 'RESULT_PURGING', resultAvailable: false }), true)
  assert.equal(canBindReportExport({ status: 'RESULT_PURGED', resultAvailable: false }), true)
  assert.equal(canBindReportExport({ status: 'FAILED', resultAvailable: false }), false)
})

test('completed runs can retry binding the user export after a transient failure', () => {
  const run = { status: 'RESULT_PURGED', resultAvailable: false }
  assert.equal(canRetryReportExportBinding(run, null), true)
  assert.equal(canRetryReportExportBinding(run, { status: 'READY' }), false)
  assert.equal(canRetryReportExportBinding({ status: 'FAILED', resultAvailable: false }, null), false)
})

test('new report run state clears the frozen snapshot and restores parameter defaults', () => {
  const state = buildNewReportRunState([{
    code: 'count', logicalType: 'integer', defaultValue: 5, systemInjected: false,
  }])
  assert.deepEqual(state, {
    values: { count: '5' }, run: null, result: null, reportExport: null,
    resultQuery: { filters: [], sort: [] }, appliedQuery: { filters: [], sort: [] },
    cursorHistory: [''], cursorIndex: 0, filtersOpen: false, parametersOpen: true,
    operation: { busy: false, error: '' },
  })
})

test('run, result and export parsers preserve cursor and large numeric strings', () => {
  const runPayload = { id: 31, runUuid: 'run-uuid', definitionId: 9, versionId: 23, status: 'SUCCEEDED', rowCount: 1, resultAvailable: true }
  const run = parseReportRun({ data: runPayload })
  assert.equal(run.status, 'SUCCEEDED')

  const page = parseReportResultPage({ data: {
    run: runPayload,
    columns: [{ fieldId: 'amount-id', code: 'amount', header: '金额', valueType: 'decimal', filterable: true, sortable: true, allowedOperators: ['EQ', 'BETWEEN'] }],
    rows: [{ key: '1', values: { amount: '9999999999999999.01' } }],
    pagination: { pageSize: 100, hasMore: true, nextCursor: 'signed.cursor' },
  } })
  assert.equal(page.rows[0].values.amount, '9999999999999999.01')
  assert.equal(page.pagination.nextCursor, 'signed.cursor')
	assert.deepEqual(page.columns[0].allowedOperators, ['EQ', 'BETWEEN'])

  const reportExport = parseReportExport({ data: { id: 41, runId: 31, exportUuid: 'export-uuid', status: 'READY', exportedRows: 1, canDownload: true } })
  assert.equal(reportExport.status, 'READY')
  assert.equal(reportExport.canDownload, true)
})

test('parseReportExportPage preserves archive and purge fields', () => {
  const page = parseReportExportPage({ data: {
    items: [{
      id: 41, runId: 31, exportUuid: 'export-uuid', reportName: '门店销售日报', status: 'READY',
      exportedRows: 120, purgedRows: 80, purgeStartedAt: '2026-08-13T09:59:00Z',
      expiresAt: '2026-08-16T10:00:00Z', canDownload: true,
    }],
    hasMore: true,
    nextAfterId: 41,
  } })

  assert.equal(page.items[0].reportName, '门店销售日报')
  assert.equal(page.items[0].purgedRows, 80)
  assert.equal(page.items[0].purgeStartedAt, '2026-08-13T09:59:00Z')
  assert.equal(page.hasMore, true)
  assert.equal(page.nextAfterId, 41)
})

test('parseReportExportPage rejects a missing cursor when more rows exist', () => {
  assert.throws(() => parseReportExportPage({ data: { items: [], hasMore: true } }))
})

test('parseReportResultPage rejects a missing signed cursor', () => {
  const run = { id: 31, runUuid: 'run-uuid', definitionId: 9, versionId: 23, status: 'SUCCEEDED', resultAvailable: true }
  assert.throws(() => parseReportResultPage({ data: { run, columns: [], rows: [], pagination: { pageSize: 100, hasMore: true } } }))
})

test('parseReportDraft preserves parameter, field and excel mappings', () => {
  const draft = parseReportDraft({ data: {
    id: 9, code: 'sales_report', name: '销售报表', datasourceId: 3, status: 'DRAFT', lockVersion: 4,
    procedure: { owner: 'BI', package: 'REPORT_PKG', name: 'SALES' },
    result: { tableOwner: 'BI', tableName: 'REPORT_RESULT' },
    callTemplate: 'BEGIN BI.REPORT_PKG.SALES({{runId}}, {{storeCode}}); END;',
    parameters: [{ code: 'runId', label: '运行编号', displayOrder: 0, controlType: 'TEXT', logicalType: 'string', procedureArgName: 'P_RUN_ID', position: 1, oracleType: 'VARCHAR2', precision: 38, scale: 0, required: true, systemInjected: true, normalizer: { trim: true }, valueSource: { source: 'run_id' }, nullPolicy: 'TYPED_NULL' }],
    columns: [{ fieldId: '11111111-1111-4111-8111-111111111111', logicalCode: 'amount', databaseColumn: 'AMOUNT', sourceOracleType: 'NUMBER', precision: 18, scale: 2, valueType: 'decimal', previewHeader: '金额', excelHeader: '含税金额', displayOrder: 2, exportOrder: 1, previewVisible: true, exportVisible: true, exportAllowed: true, dictionaryVersion: { version: 'v2' } }],
    grants: [{ subjectType: 'ROLE', subjectId: 7, actions: ['QUERY', 'EXPORT'] }],
  } })
  assert.equal(draft.parameters[0].systemInjected, true)
  assert.equal(draft.parameters[0].precision, 38)
  assert.deepEqual(draft.parameters[0].normalizer, { trim: true })
  assert.deepEqual(draft.parameters[0].valueSource, { source: 'RUN_ID' })
  assert.equal(draft.columns[0].databaseColumn, 'AMOUNT')
  assert.equal(draft.columns[0].precision, 18)
  assert.equal(draft.columns[0].displayOrder, 2)
  assert.deepEqual(draft.columns[0].dictionaryVersion, { version: 'v2' })
  assert.equal(draft.columns[0].excelHeader, '含税金额')
  assert.deepEqual(draft.grants[0].actions, ['QUERY', 'EXPORT'])
  assert.equal(draft.executionMode, 'TABLE_SNAPSHOT')
})

test('REF CURSOR draft keeps JSON condition display names and automatic argument bindings', () => {
  const draft = parseReportDraft({ data: {
    id: 10, code: 'supplier_report', name: '供应商报表', datasourceId: 3, status: 'DRAFT', lockVersion: 2,
    executionMode: 'REF_CURSOR',
    procedure: { owner: 'BI', package: 'REPORT_PKG', name: 'SUPPLIER', jsonInputArgName: 'P_PAYLOAD', resultCursorArgName: 'P_RESULT' },
    inputSchema: {
      c_supplier_id: { type: 'varchar2', displayName: '供应商', control: 'multi_select', required: true, multiple: true, example: ['a', 'b'] },
      datein_begin: { type: 'date', displayName: '开始日期', control: 'date', example: '20260504' },
    },
    columns: [], grants: [], parameters: [], result: {}, callTemplate: '',
  } })
  assert.equal(draft.executionMode, 'REF_CURSOR')
  assert.equal(draft.procedure.jsonInputArgName, 'P_PAYLOAD')
  assert.equal(draft.inputSchema.c_supplier_id.displayName, '供应商')
  assert.equal(draft.inputSchema.c_supplier_id.type, 'list[str]')
  assert.equal(draft.inputSchema.c_supplier_id.control, 'SELECT')
  assert.equal(draft.inputSchema.datein_begin.type, 'str')
  assert.equal(draft.inputSchema.datein_begin.control, 'DATE')
  assert.equal(draft.inputSchema.datein_begin.format, 'YYYYMMDD')
  assert.deepEqual(draft.inputSchema.c_supplier_id.example, ['a', 'b'])
})

test('JSON result-table draft keeps one input binding and the Oracle snapshot table', async () => {
  const payload = { data: {
    id: 11, code: 'table_json_report', name: '结果表报表', datasourceId: 3, status: 'DRAFT', lockVersion: 3,
    executionMode: 'TABLE_SNAPSHOT',
    procedure: { owner: 'BI', package: 'REPORT_PKG', name: 'BUILD_RESULT', jsonInputArgName: 'P_PAYLOAD', resultCursorArgName: '' },
    inputSchema: { store_id: { type: 'VARCHAR2', displayName: '门店' } },
    result: { tableOwner: 'BI', tableName: 'REPORT_RESULT' },
    columns: [{ fieldId: '11111111-1111-4111-8111-111111111111', logicalCode: 'amount', databaseColumn: 'AMOUNT', sourceOracleType: 'NUMBER', valueType: 'decimal', previewHeader: '金额', excelHeader: '金额', displayOrder: 0, exportOrder: 0, previewVisible: true, exportVisible: true, exportAllowed: true }],
    grants: [], parameters: [], callTemplate: 'BEGIN BI.REPORT_PKG.BUILD_RESULT(P_PAYLOAD => :payload); END;',
  } }
  const draft = parseReportDraft(payload)
  assert.equal(draft.procedure.resultCursorArgName, '')
  assert.equal(draft.result.tableName, 'REPORT_RESULT')
  assert.equal(draft.inputSchema.store_id.displayName, '门店')
  draft.inputSchema.store_id = { ...draft.inputSchema.store_id, type: 'list[str]', displayName: '门店筛选', required: true, control: 'SELECT', allowedValues: ['S001', 'S002'] }
  const requests = []
  const client = async (path, options) => { requests.push({ path, options }); return { ok: true, data: payload } }
  await saveReportDraft(client, draft)
  assert.equal(requests[0].options.body.executionMode, 'TABLE_SNAPSHOT')
  assert.deepEqual(requests[0].options.body.parameters, [])
  assert.deepEqual(requests[0].options.body.inputSchema.store_id, { type: 'list[str]', displayName: '门店筛选', required: true, control: 'SELECT', allowedValues: ['S001', 'S002'] })
  assert.deepEqual(requests[0].options.body.result, { tableOwner: 'BI', tableName: 'REPORT_RESULT' })
  assert.equal(requests[0].options.body.callTemplate, 'BEGIN BI.REPORT_PKG.BUILD_RESULT(P_PAYLOAD => :payload); END;')
})

test('dirty report configuration saves the selected date format before publication', async () => {
  const hash = 'a'.repeat(64)
  const savedPayload = { data: {
    id: 11, code: 'table_json_report', name: '结果表报表', datasourceId: 3, status: 'DRAFT', lockVersion: 4,
    executionMode: 'TABLE_SNAPSHOT', procedure: { owner: 'BI', package: 'REPORT_PKG', name: 'BUILD_RESULT', jsonInputArgName: 'P_PAYLOAD', resultCursorArgName: '' },
    inputSchema: { datein_begin: { type: 'str', displayName: '开始日期', control: 'DATE', format: 'YYYYMMDD' } },
    result: { tableOwner: 'BI', tableName: 'REPORT_RESULT' },
    columns: [{ fieldId: '11111111-1111-4111-8111-111111111111', logicalCode: 'amount', databaseColumn: 'AMOUNT', sourceOracleType: 'NUMBER', valueType: 'decimal', previewHeader: '金额', excelHeader: '金额', displayOrder: 0, exportOrder: 0, previewVisible: true, exportVisible: true, exportAllowed: true }],
    grants: [], parameters: [], callTemplate: 'BEGIN BI.REPORT_PKG.BUILD_RESULT(P_PAYLOAD => :payload); END;',
  } }
  const draft = parseReportDraft(savedPayload)
  draft.lockVersion = 3
  const requests = []
  const client = async (path, options) => {
    requests.push({ path, options })
    if (path.endsWith('/publish')) return { ok: true, data: { data: { definitionId: 11, versionId: 8, version: 4, status: 'PUBLISHED', contractHash: hash, publishedAt: '2026-08-14T14:30:00Z' } } }
    return { ok: true, data: savedPayload }
  }
  const result = await saveAndPublishReportDraft(client, draft)
  assert.equal(result.ok, true)
  assert.equal(requests[0].path, '/v1/reports/11')
  assert.equal(requests[0].options.body.inputSchema.datein_begin.format, 'YYYYMMDD')
  assert.equal(requests[1].path, '/v1/reports/11/publish')
  assert.equal(requests[1].options.body.expectedLockVersion, 4)

  const saveFailureRequests = []
  const saveFailure = await saveAndPublishReportDraft(async (path, options) => {
    saveFailureRequests.push({ path, options })
    return { ok: false, error: { message: '草稿保存失败' } }
  }, draft)
  assert.deepEqual(saveFailure, { ok: false, error: '草稿保存失败' })
  assert.equal(saveFailureRequests.length, 1)

  const publishFailureRequests = []
  const publishFailure = await saveAndPublishReportDraft(async (path, options) => {
    publishFailureRequests.push({ path, options })
    if (path.endsWith('/publish')) return { ok: false, error: { message: '发布失败' } }
    return { ok: true, data: savedPayload }
  }, draft)
  assert.equal(publishFailure.ok, false)
  assert.equal(publishFailure.error, '发布失败')
  assert.equal(publishFailure.draft.lockVersion, 4)
  assert.equal(publishFailureRequests.length, 2)
})

test('condition JSON requires a filter display name and normalizes supported types', () => {
  const schema = parseReportInputSchemaDocument({ a: { type: 'varchar2', displayName: '门店', control: 'text', required: true, example: '01' } })
  assert.deepEqual(schema.a, { type: 'str', displayName: '门店', control: 'TEXT', required: true, example: '01' })
  assert.throws(() => parseReportInputSchemaDocument({ a: { type: 'VARCHAR2' } }), /筛选显示名/)
  assert.throws(() => parseReportInputSchemaDocument({ a: { type: 'VARCHAR2', displayName: '门店', unknown: true } }), /未知配置/)
})

test('condition JSON keeps a configurable display order across save and reload', () => {
	const schema = parseReportInputSchemaDocument({
		store: { type: 'str', displayName: '门店' },
		date: { type: 'str', displayName: '日期' },
		product: { type: 'str', displayName: '商品' },
	})
	const moved = moveReportInputField(schema, 'product', -1)
	assert.deepEqual(orderedReportInputEntries(moved).map(([code]) => code), ['store', 'product', 'date'])
	assert.deepEqual(Object.values(moved).map((field) => field.displayOrder), [0, 1, 2])
	const reloaded = parseReportInputSchemaDocument(JSON.parse(JSON.stringify(moved)))
	assert.deepEqual(orderedReportInputEntries(reloaded).map(([code]) => code), ['store', 'product', 'date'])
	assert.throws(() => parseReportInputSchemaDocument({ store: { type: 'str', displayName: '门店', displayOrder: 0 }, date: { type: 'str', displayName: '日期' } }), /displayOrder/)
})

test('condition JSON validates metadata types, date precision and exact numeric literals before save', () => {
  assert.throws(() => parseReportInputSchemaDocument({ amount: { type: 'number', displayName: '金额', default: '12.5' } }), /默认值/)
  assert.throws(() => parseReportInputSchemaDocument({ stores: { type: 'list[str]', displayName: '门店', example: ['S001', 2] } }), /示例值/)
  assert.throws(() => parseReportInputSchemaDocument({ day: { type: 'str', displayName: '日期', control: 'DATE', format: 'YYYYMMDD', default: '20260504123045' } }), /日期格式/)
  assert.throws(() => parseReportInputSchemaDocument({ day: { type: 'str', displayName: '日期', control: 'DATE', format: 'BAD' } }), /日期格式不受支持/)
  assert.throws(() => parseReportInputSchemaText('{"amount":{"type":"number","displayName":"金额","default":9007199254740993}}'), /安全数字范围/)
  assert.throws(() => parseReportInputSchemaText('{"amount":{"type":"number","displayName":"金额","default":0.10000000000000001}}'), /安全数字范围/)
})

test('condition JSON sends only configured canonical values and formats date controls', () => {
  const schema = parseReportInputSchemaDocument({
    supplier: { type: 'str', displayName: '供应商' },
    amount: { type: 'number', displayName: '金额' },
    enabled: { type: 'bool', displayName: '启用' },
    stores: { type: 'list[str]', displayName: '门店' },
    levels: { type: 'list[number]', displayName: '等级' },
    flags: { type: 'list[bool]', displayName: '标记' },
    options: { type: 'json', displayName: '扩展条件' },
    dayCompact: { type: 'str', displayName: '业务日期', control: 'DATE', format: 'YYYYMMDD' },
    dayDashed: { type: 'str', displayName: '结束日期', control: 'DATE', format: 'YYYY-MM-DD' },
    timeCompact: { type: 'str', displayName: '执行时间', control: 'DATETIME', format: 'YYYYMMDDHHmmss' },
    timeReadable: { type: 'str', displayName: '完成时间', control: 'DATETIME', format: 'YYYY-MM-DD HH:mm:ss' },
    timeISO: { type: 'str', displayName: '同步时间', control: 'DATETIME', format: 'ISO8601' },
  })
  assert.deepEqual(buildReportConditions(schema, {
    supplier: 'A001', amount: '12.5', enabled: true, stores: '["S001","S002"]', levels: '[1,2]', flags: '[true,false]', options: '{"active":true}',
    dayCompact: '2026-05-04', dayDashed: '2026-05-05', timeCompact: '2026-05-04T13:25', timeReadable: '2026-05-04T13:25:06', timeISO: '2026-05-04T13:25',
    report_id: 99, run_id: 100,
  }), { ok: true, conditions: {
    supplier: 'A001', amount: 12.5, enabled: true, stores: ['S001', 'S002'], levels: [1, 2], flags: [true, false], options: { active: true },
    dayCompact: '20260504', dayDashed: '2026-05-05', timeCompact: '20260504132500', timeReadable: '2026-05-04 13:25:06', timeISO: '2026-05-04T13:25:00',
  } })
  assert.deepEqual(buildReportConditions(parseReportInputSchemaDocument({ day: { type: 'str', displayName: '日期', control: 'DATE' } }), { day: '2026-05-04' }), { ok: true, conditions: { day: '20260504' } })
  assert.deepEqual(buildReportConditions(schema, { levels: '["1"]' }), { ok: false, error: '等级 与 list[number] 类型不匹配。' })
  assert.deepEqual(buildReportConditions(schema, { dayCompact: '2026-02-30' }), { ok: false, error: '业务日期 必须填写有效日期。' })
})

test('optional conditions keep typed empty values instead of omitting fields', () => {
  const schema = parseReportInputSchemaDocument({
    supplier_ids: { type: 'list[str]', displayName: '供应商' },
    store_id: { type: 'str', displayName: '门店' },
    report_date: { type: 'str', displayName: '报表日期', control: 'DATE', format: 'YYYYMMDD' },
    minimum: { type: 'number', displayName: '最小值' },
    enabled: { type: 'bool', displayName: '启用' },
    options: { type: 'json', displayName: '扩展条件' },
    default_store: { type: 'str', displayName: '默认门店', default: 'S001' },
  })
  assert.deepEqual(buildReportConditions(schema, {}), { ok: true, conditions: {
    supplier_ids: [], store_id: '', report_date: '', minimum: null, enabled: null, options: null, default_store: 'S001',
  } })
})

test('number conditions reject values that JavaScript cannot preserve safely', () => {
  const schema = parseReportInputSchemaDocument({
    amount: { type: 'number', displayName: '金额' },
    levels: { type: 'list[number]', displayName: '等级' },
  })
  const unsafeMessage = ' 超出 JavaScript 安全数字范围或无法无损表示，请改用 str 类型。'

  assert.deepEqual(buildReportConditions(schema, { amount: '9007199254740991' }), { ok: true, conditions: { amount: 9007199254740991, levels: [] } })
  assert.deepEqual(buildReportConditions(schema, { amount: '1.25e3' }), { ok: true, conditions: { amount: 1250, levels: [] } })
  assert.deepEqual(buildReportConditions(schema, { amount: '9007199254740993' }), { ok: false, error: `金额${unsafeMessage}` })
  assert.deepEqual(buildReportConditions(schema, { amount: '0.10000000000000001' }), { ok: false, error: `金额${unsafeMessage}` })
  assert.deepEqual(buildReportConditions(schema, { levels: '[1,9007199254740993]' }), { ok: false, error: `等级${unsafeMessage}` })
  assert.deepEqual(buildReportConditions(schema, { levels: '[0.1,0.10000000000000001]' }), { ok: false, error: `等级${unsafeMessage}` })
  assert.deepEqual(buildReportConditions(schema, { levels: '[1e309]' }), { ok: false, error: `等级${unsafeMessage}` })
})

test('formatted date defaults use HTML input values and convert back before submission', () => {
  const schema = parseReportInputSchemaDocument({
    day: { type: 'str', displayName: '日期', control: 'DATE', format: 'YYYYMMDD', default: '20260504', allowedValues: ['20260504', '20260505'] },
    compactTime: { type: 'str', displayName: '紧凑时间', control: 'DATETIME', format: 'YYYYMMDDHHmmss', default: '20260504123045' },
    readableTime: { type: 'str', displayName: '可读时间', control: 'DATETIME', format: 'YYYY-MM-DD HH:mm:ss', default: '2026-05-04 12:30:45' },
    isoTime: { type: 'str', displayName: 'ISO 时间', control: 'DATETIME', format: 'ISO8601', default: '2026-05-04T12:30:45' },
  })
  const values = initialReportConditionValues(schema)
  assert.deepEqual(values, { day: '2026-05-04', compactTime: '2026-05-04T12:30:45', readableTime: '2026-05-04T12:30:45', isoTime: '2026-05-04T12:30:45' })
  assert.deepEqual(buildReportConditions(schema, values), { ok: true, conditions: { day: '20260504', compactTime: '20260504123045', readableTime: '2026-05-04 12:30:45', isoTime: '2026-05-04T12:30:45' } })
  assert.deepEqual(buildReportConditions(schema, { ...values, day: '2026-05-05' }), { ok: true, conditions: { day: '20260505', compactTime: '20260504123045', readableTime: '2026-05-04 12:30:45', isoTime: '2026-05-04T12:30:45' } })
})

test('Excel JSON mapping and table edits share the existing columns contract', () => {
  const mapping = parseExcelMappingDocument({ a: 'id', amount: '金额' })
  const columns = applyExcelMapping([], mapping, (() => { let index = 0; return () => `00000000-0000-4000-8000-${String(++index).padStart(12, '0')}` })())
  assert.equal(columns[0].databaseColumn, 'a')
  assert.equal(columns[0].excelHeader, 'id')
  assert.equal(columns[1].exportOrder, 1)
  assert.deepEqual(excelMappingFromColumns(columns), mapping)
  const edited = applyExcelMapping(columns, { a: '编号' }, () => { throw new Error('existing field must keep its stable id') })
  assert.equal(edited[0].fieldId, columns[0].fieldId)
  assert.equal(edited[0].excelHeader, '编号')
  const renamed = renameExcelMappingField(columns, 'a', 'supplier_id')
  assert.equal(renamed[0].fieldId, columns[0].fieldId)
  assert.equal(renamed[0].databaseColumn, 'supplier_id')
})

test('result table schema maps every field and keeps edited headers', () => {
  let index = 0
  const createFieldId = () => `00000000-0000-4000-8000-${String(++index).padStart(12, '0')}`
  const schema = [
    { name: 'RUN_ID', position: 1, oracleType: 'VARCHAR2', dataLength: 36, precision: null, scale: null, nullable: false },
    { name: 'ID', position: 2, oracleType: 'NUMBER', dataLength: 22, precision: 18, scale: 0, nullable: false },
    { name: 'SUPPLIER_ID', position: 3, oracleType: 'VARCHAR2', dataLength: 64, precision: null, scale: null, nullable: true },
  ]
  const generated = reportColumnsFromResultSchema(schema, createFieldId)
  assert.deepEqual(generated.map((column) => column.databaseColumn), ['RUN_ID', 'ID', 'SUPPLIER_ID'])
  assert.equal(generated[0].sourceOracleType, 'VARCHAR2')
  assert.equal(generated[1].sourceOracleType, 'NUMBER')
  const customized = generated.map((column) => column.databaseColumn === 'SUPPLIER_ID'
    ? { ...column, excelHeader: '供应商编码', previewHeader: '供应商编码' }
    : column)
  const reconciled = reconcileReportColumnsWithResultSchema(schema, customized, () => { throw new Error('existing field must keep its stable id') })
  const supplier = reconciled.find((column) => column.databaseColumn === 'SUPPLIER_ID')
  assert.equal(supplier.fieldId, generated[2].fieldId)
  assert.equal(supplier.excelHeader, '供应商编码')
})

test('result table metadata parsers and requests enforce the Oracle table contract', async () => {
  const table = { owner: 'REPORT', name: 'SALES_RESULT', qualifiedName: 'REPORT.SALES_RESULT', columnCount: 3 }
  const columns = [
    { name: 'RUN_ID', position: 1, oracleType: 'VARCHAR2', dataLength: 36, precision: null, scale: null, nullable: false },
    { name: 'ID', position: 2, oracleType: 'NUMBER', dataLength: 22, precision: 18, scale: 0, nullable: false },
    { name: 'AMOUNT', position: 3, oracleType: 'NUMBER', dataLength: 22, precision: 18, scale: 2, nullable: true },
  ]
  assert.equal(parseReportResultTablePage({ data: { items: [table], hasMore: true, nextAfter: 'opaque' } }).items[0].qualifiedName, table.qualifiedName)
  assert.equal(parseReportResultTableSchema({ data: { table, columns } }).columns[2].precision, 18)
  assert.throws(() => parseReportResultTableSchema({ data: { table, columns: [columns[1], columns[0], columns[2]] } }), /column order/)

  const requests = []
  const client = async (path, options) => {
    requests.push({ path, options })
    return path.includes('result-table-schema')
      ? { ok: true, data: { data: { table, columns } } }
      : { ok: true, data: { data: { items: [table], hasMore: false, nextAfter: '' } } }
  }
  await getReportResultTables(client, 3, { owner: ' REPORT ', search: ' sales ', after: 'opaque', limit: 200 })
  await getReportResultTableSchema(client, 3, { owner: 'REPORT', name: 'SALES_RESULT' })
  assert.equal(requests[0].path, '/v1/report-datasources/3/result-tables?owner=REPORT&search=sales&after=opaque&limit=100')
  assert.equal(requests[1].path, '/v1/report-datasources/3/result-table-schema?owner=REPORT&name=SALES_RESULT')
})

test('procedure catalog and signature parsers enforce the JSON cursor protocol contract', () => {
  const page = parseReportProcedurePage({ data: { items: [{ owner: 'REPORT', package: 'PKG', name: 'BUILD', overload: '1', argumentCount: 2, qualifiedName: 'REPORT.PKG.BUILD #1' }], hasMore: true, nextAfter: 'cursor' } })
  assert.equal(page.items[0].qualifiedName, 'REPORT.PKG.BUILD #1')
  const signature = parseReportProcedureSignature({ data: {
    procedure: page.items[0], allSupported: true, protocolReady: true, inputArgName: 'P_PAYLOAD', outputArgName: 'P_RESULT', callTemplate: 'BEGIN ... END;', blockingReasons: [],
    arguments: [
      { name: 'P_PAYLOAD', position: 1, sequence: 1, direction: 'IN', oracleType: 'CLOB', dataLength: 4000, precision: null, scale: null, typeOwner: '', typeName: '', defaulted: false, supported: true, suggestedCode: 'payload', suggestedLogicalType: 'json', suggestedControlType: 'TEXTAREA', suggestedSystemValue: '', role: 'JSON_INPUT' },
      { name: 'P_RESULT', position: 2, sequence: 2, direction: 'OUT', oracleType: 'REF CURSOR', dataLength: null, precision: null, scale: null, typeOwner: '', typeName: '', defaulted: false, supported: true, suggestedCode: 'result', suggestedLogicalType: 'cursor', suggestedControlType: '', suggestedSystemValue: '', role: 'RESULT_CURSOR' },
    ],
  } })
  assert.equal(signature.protocolReady, true)
  assert.equal(signature.outputArgName, 'P_RESULT')
  assert.throws(() => parseReportProcedureSignature({ data: { ...signature, blockingReasons: ['blocked'] } }))
})

test('procedure signature accepts one JSON input with an R_ERROR output', () => {
  const signature = parseReportProcedureSignature({ data: {
    procedure: { owner: 'REPORT', package: 'PKG', name: 'BUILD', overload: '', argumentCount: 2, qualifiedName: 'REPORT.PKG.BUILD' },
    allSupported: true, protocolReady: true, inputArgName: 'P_PAYLOAD', outputArgName: '', callTemplate: "DECLARE error_output_2 VARCHAR2(32767); BEGIN REPORT.PKG.BUILD(P_PAYLOAD => :payload, R_ERROR => error_output_2); IF error_output_2 IS NOT NULL AND TRIM(SUBSTR(error_output_2, 1, 500)) <> '执行成功' THEN RAISE_APPLICATION_ERROR(-20001, SUBSTR(error_output_2, 1, 500)); END IF; END;", blockingReasons: [],
    arguments: [
      { name: 'P_PAYLOAD', position: 1, sequence: 1, direction: 'IN', oracleType: 'CLOB', dataLength: null, precision: null, scale: null, typeOwner: '', typeName: '', defaulted: false, supported: true, suggestedCode: 'payload', suggestedLogicalType: 'json', suggestedControlType: 'TEXTAREA', suggestedSystemValue: '', role: 'JSON_INPUT' },
      { name: 'R_ERROR', position: 2, sequence: 2, direction: 'OUT', oracleType: 'VARCHAR2', dataLength: null, precision: null, scale: null, typeOwner: '', typeName: '', defaulted: false, supported: true, suggestedCode: 'rError', suggestedLogicalType: '', suggestedControlType: '', suggestedSystemValue: '', role: 'ERROR_OUTPUT' },
    ],
  } })
  assert.equal(signature.protocolReady, true)
  assert.equal(signature.inputArgName, 'P_PAYLOAD')
  assert.equal(signature.outputArgName, '')
  assert.equal(signature.arguments[1].role, 'ERROR_OUTPUT')
})

test('procedure API encodes discovery filters and REF CURSOR draft save omits legacy contracts', async () => {
  const requests = []
  const client = async (path, options) => {
    requests.push({ path, options })
    if (path.includes('procedure-signature')) return { ok: true, data: { data: {
      procedure: { owner: 'REPORT', package: 'PKG', name: 'BUILD', overload: '', argumentCount: 0, qualifiedName: 'REPORT.PKG.BUILD' },
      arguments: [], allSupported: false, protocolReady: false, inputArgName: '', outputArgName: '', callTemplate: '', blockingReasons: ['缺少参数'],
    } } }
    if (path.includes('/procedures?')) return { ok: true, data: { data: { items: [], hasMore: false, nextAfter: '' } } }
    return { ok: true, data: { data: {
      id: 12, code: 'json_report', name: 'JSON 报表', datasourceId: 3, status: 'DRAFT', lockVersion: 1, executionMode: 'REF_CURSOR',
      procedure: { owner: 'REPORT', package: 'PKG', name: 'BUILD', jsonInputArgName: 'P_PAYLOAD', resultCursorArgName: 'P_RESULT' },
      inputSchema: { store_id: { type: 'VARCHAR2', displayName: '门店' } }, parameters: [], columns: [], grants: [], result: {}, callTemplate: '',
    } } }
  }
  await getReportProcedures(client, 3, { owner: ' REPORT ', search: ' daily ', limit: 200 })
  await getReportProcedureSignature(client, 3, { owner: 'REPORT', package: 'PKG', name: 'BUILD', overload: '' })
  const draft = parseReportDraft({ data: { id: 12, code: 'json_report', name: 'JSON 报表', datasourceId: 3, status: 'DRAFT', lockVersion: 1, executionMode: 'REF_CURSOR', procedure: { owner: 'REPORT', package: 'PKG', name: 'BUILD', jsonInputArgName: 'P_PAYLOAD', resultCursorArgName: 'P_RESULT' }, inputSchema: { store_id: { type: 'VARCHAR2', displayName: '门店' } }, parameters: [], columns: [], grants: [], result: {}, callTemplate: '' } })
  await saveReportDraft(client, draft)
  assert.equal(requests[0].path, '/v1/report-datasources/3/procedures?owner=REPORT&search=daily&limit=100')
  assert.equal(requests[1].path, '/v1/report-datasources/3/procedure-signature?owner=REPORT&name=BUILD&package=PKG')
  assert.equal(requests[2].options.body.executionMode, 'REF_CURSOR')
  assert.deepEqual(requests[2].options.body.parameters, [])
  assert.deepEqual(requests[2].options.body.result, {})
  assert.equal(requests[2].options.body.callTemplate, '')
  assert.equal(requests[2].options.body.inputSchema.store_id.displayName, '门店')
})

test('publication parser keeps only the safe Oracle validation summary', () => {
  const hash = 'a'.repeat(64)
  const publication = parsePublication({ data: {
    definitionId: 9, versionId: 23, version: 3, status: 'PUBLISHED', contractHash: hash, publishedAt: '2026-08-13T08:00:01Z',
    validation: {
      validatedAt: '2026-08-13T08:00:00Z',
      procedure: { owner: 'REPORT', package: 'PKG_SALES', name: 'BUILD_REPORT', overload: '', argumentCount: 2, signatureHash: hash, password: 'must-not-pass' },
      result: { tableOwner: 'REPORT', tableName: 'SALES_RESULT', columnCount: 12, schemaHash: hash, dsn: 'must-not-pass' },
      snapshot: { resultTableValidated: true },
      export: { exportableColumnCount: 8, schemaHash: hash },
    },
  } })
  assert.equal(publication.validation.procedure.argumentCount, 2)
  assert.equal(publication.validation.result.columnCount, 12)
  assert.equal(publication.validation.snapshot.resultTableValidated, true)
  assert.equal(Object.hasOwn(publication.validation.procedure, 'password'), false)
  assert.equal(Object.hasOwn(publication.validation.result, 'dsn'), false)
  assert.equal(parsePublication({ data: { definitionId: 9, versionId: 23, version: 3, status: 'PUBLISHED', contractHash: hash } }).validation, null)
  assert.throws(() => parsePublication({ data: { definitionId: 9, versionId: 23, version: 3, contractHash: hash, validation: {} } }))
  for (const validation of [
    { ...publication.validation, procedure: { ...publication.validation.procedure, name: '' } },
    { ...publication.validation, result: { ...publication.validation.result, columnCount: 0 } },
    { ...publication.validation, export: { ...publication.validation.export, schemaHash: 'a'.repeat(63) } },
    { ...publication.validation, snapshot: { ...publication.validation.snapshot, resultTableValidated: false } },
  ]) assert.throws(() => parsePublication({ data: { definitionId: 9, versionId: 23, version: 3, status: 'PUBLISHED', contractHash: hash, validation } }))
})

test('version parsers enforce cursor and structured summary differences', () => {
  const version = (id, number) => ({ id, version: number, status: 'PUBLISHED', contractFingerprint: 'a'.repeat(12), parameterCount: 2, columnCount: 4, grantCount: 1 })
  const page = parseReportVersionPage({ data: { items: [version(23, 2), version(11, 1)], hasMore: true, nextAfterId: 11 } })
  assert.equal(page.items[0].version, 2)
  assert.throws(() => parseReportVersionPage({ data: { items: [version(23, 2)], hasMore: true, nextAfterId: 99 } }))
  const sections = [
    { key: 'procedure', label: '存储过程', changes: [] },
    { key: 'parameters', label: '{{形参}}', changes: [{ kind: 'CHANGED', key: 'parameterCount', label: '参数数量', before: 2, after: 3 }] },
    { key: 'results', label: '结果字段与 Excel', changes: [] },
    { key: 'excel', label: 'Excel 契约', changes: [{ kind: 'CHANGED', key: 'exportSchemaHash', label: 'Excel Schema', before: 'b'.repeat(12), after: 'c'.repeat(12) }] },
    { key: 'permissions', label: '权限', changes: [] },
  ]
  const diff = parseReportVersionDiff({ data: { base: version(11, 1), target: version(23, 2), sections } })
  assert.equal(diff.sections[1].label, '筛选条件')
  assert.equal(diff.sections[1].changes[0].after, 3)
  assert.equal(diff.sections[3].changes[0].before, 'b'.repeat(12))
  assert.throws(() => parseReportVersionDiff({ data: { base: version(11, 1), target: version(23, 2), sections: sections.map((section) => section.key === 'excel' ? { ...section, changes: [{ ...section.changes[0], before: { leaked: true } }] } : section) } }))
  assert.throws(() => parseReportVersionPage({ data: { items: [{ ...version(23, 2), contractFingerprint: '' }], hasMore: false, nextAfterId: 23 } }))
  assert.throws(() => parseReportVersionPage({ data: { items: [{ ...version(23, 2), contractFingerprint: 'a'.repeat(64) }], hasMore: false, nextAfterId: 23 } }))
  assert.throws(() => parseReportVersionPage({ data: { items: [{ ...version(23, 2), contractFingerprint: 'a'.repeat(13) }], hasMore: false, nextAfterId: 23 } }))
  assert.throws(() => parseReportVersionPage({ data: { items: [version(11, 1), version(23, 2)], hasMore: false, nextAfterId: 23 } }))
})

test('historical version detail and activation use the selected immutable version', async () => {
  const summary = { id: 18, version: 2, status: 'PUBLISHED', contractFingerprint: 'a'.repeat(12), parameterCount: 1, columnCount: 1, grantCount: 1 }
  const payload = { data: { summary, configuration: {
    datasourceId: 4,
    executionMode: 'TABLE_SNAPSHOT',
    procedure: { owner: 'REPORT', package: 'PKG_SALES', name: 'BUILD', overload: '' },
    result: { tableOwner: 'REPORT', tableName: 'SALES_RESULT' },
    callTemplate: 'BEGIN REPORT.PKG_SALES.BUILD(); END;',
    parameters: [{ code: 'store', label: '门店', displayOrder: 1, position: 1, procedureArgName: 'P_STORE', oracleType: 'VARCHAR2' }],
    columns: [{ fieldId: '11111111-1111-4111-8111-111111111111', logicalCode: 'amount', databaseColumn: 'AMOUNT', excelHeader: '金额', exportOrder: 1, exportVisible: true, exportAllowed: true, valueType: 'decimal' }],
    grants: [{ subjectType: 'ROLE', subjectId: 2, actions: ['QUERY', 'EXPORT'] }],
  } } }
  const detail = parseReportVersionDetail(payload)
  assert.equal(detail.summary.id, 18)
  assert.equal(detail.configuration.procedure.name, 'BUILD')
  assert.equal(detail.configuration.columns[0].valueType, 'decimal')

  const requests = []
  const hash = 'b'.repeat(64)
  const client = async (path, options) => {
    requests.push({ path, options })
    if (options.method === 'GET') return { ok: true, data: payload }
    return { ok: true, data: { data: { definitionId: 9, versionId: 18, version: 2, status: 'PUBLISHED', contractHash: hash } } }
  }
  assert.equal((await getReportVersion(client, 9, 18)).ok, true)
  assert.equal((await activateReportVersion(client, 9, 18, 4)).ok, true)
  assert.equal(requests[0].path, '/v1/reports/9/versions/18')
  assert.equal(requests[1].path, '/v1/reports/9/active-version')
  assert.equal(requests[1].options.method, 'PUT')
  assert.deepEqual(requests[1].options.body, { versionId: 18, expectedLockVersion: 4 })
})

test('version request guard invalidates superseded and cancelled responses', () => {
  const guard = createLatestRequestGuard()
  const first = guard.begin()
  const second = guard.begin()
  assert.equal(first.signal.aborted, true)
  assert.equal(first.isCurrent(), false)
  assert.equal(second.isCurrent(), true)
  guard.cancel()
  assert.equal(second.signal.aborted, true)
  assert.equal(second.isCurrent(), false)
})

test('Oracle datasource parser exposes only the public management contract', () => {
  const datasource = parseReportDatasource({ data: {
    id: 7, code: 'report_oracle', name: '经营报表库', driver: 'ORACLE', host: 'oracle.internal', port: 1521,
    serviceName: 'REPORT', sid: '', username: 'report_user', hasPassword: true, enabled: true,
    sessionTimezone: 'Asia/Shanghai', connectTimeoutSeconds: 5, queryTimeoutSeconds: 300,
    maxOpenConnections: 10, maxIdleConnections: 2, prefetchRows: 1000, arraySize: 1000,
    lastTestStatus: 'SUCCESS', lastTestedAt: '2026-08-13T08:00:00Z',
  } })
  assert.equal(datasource.id, 7)
  assert.equal(datasource.serviceName, 'REPORT')
  assert.equal(datasource.hasPassword, true)
  assert.equal(Object.hasOwn(datasource, 'password'), false)
  assert.equal(Object.hasOwn(datasource, 'passwordCiphertext'), false)
  assert.equal(Object.hasOwn(datasource, 'credentialKeyVersion'), false)
  assert.equal(Object.hasOwn(datasource, 'sessionInitJSON'), false)
  for (const forbidden of ['password', 'passwordCiphertext', 'credentialKeyVersion', 'sessionInitJSON', 'dsn']) {
    assert.throws(() => parseReportDatasource({ data: {
      id: 7, code: 'report_oracle', name: '经营报表库', driver: 'ORACLE', host: 'oracle.internal', port: 1521,
      serviceName: 'REPORT', sid: '', username: 'report_user', hasPassword: true, enabled: true, [forbidden]: 'must-not-pass',
    } }))
  }
})

test('Oracle datasource parser enforces a single Service Name or SID', () => {
  const base = { id: 7, code: 'report_oracle', name: '经营报表库', driver: 'ORACLE', host: 'oracle.internal', port: 1521, username: 'report_user', hasPassword: true, enabled: true }
  assert.throws(() => parseReportDatasource({ data: { ...base, serviceName: '', sid: '' } }))
  assert.throws(() => parseReportDatasource({ data: { ...base, serviceName: 'REPORT', sid: 'ORCL' } }))
  assert.deepEqual(parseReportDatasources({ data: { items: [{ ...base, serviceName: '', sid: 'ORCL' }] } }).map((item) => item.id), [7])
})

test('Oracle datasource test parser accepts only safe stable fields', () => {
  const result = parseReportDatasourceTest({ data: { status: 'FAILED', testedAt: '2026-08-13T08:00:00Z', latencyMs: 12, errorCode: 'AUTHENTICATION_FAILED', message: 'Oracle 用户名或密码无效', rawError: 'password=secret' } })
  assert.equal(result.errorCode, 'AUTHENTICATION_FAILED')
  assert.equal(Object.hasOwn(result, 'rawError'), false)
})

test('Oracle connection validation does not require save-only name and code fields', () => {
  const input = {
    code: '', name: '', host: 'oracle.internal', port: 1521, serviceName: 'REPORT', sid: '',
    username: 'report_user', password: 'draft-password', sessionTimezone: 'Asia/Shanghai', connectTimeoutSeconds: 5,
    queryTimeoutSeconds: 300, maxOpenConnections: 10, maxIdleConnections: 2, prefetchRows: 1000, arraySize: 1000, enabled: true,
  }
  assert.equal(validateDatasourceConnection(input, false), '')
  assert.equal(validateDatasourceSave(input, false), '请填写数据源名称。')
})

test('Oracle datasource code normalizes uppercase input and reports exact missing fields', () => {
  assert.equal(normalizeDatasourceCode('  REPORT_Oracle  '), 'report_oracle')
  const input = {
    code: 'REPORT_Oracle', name: 'Oracle 报表库', host: '', port: 1521, serviceName: 'REPORT', sid: '',
    username: 'report_user', password: 'draft-password', sessionTimezone: 'Asia/Shanghai', connectTimeoutSeconds: 5,
    queryTimeoutSeconds: 300, maxOpenConnections: 10, maxIdleConnections: 2, prefetchRows: 1000, arraySize: 1000, enabled: true,
  }
  assert.equal(validateDatasourceSave(input, false), '请填写主机地址。')
})

test('Oracle datasource validation distinguishes invalid code and saved-password reuse', () => {
  const input = {
    code: 'report-oracle', name: 'Oracle 报表库', host: 'oracle.internal', port: 1521, serviceName: 'REPORT', sid: '',
    username: 'report_user', password: '', sessionTimezone: 'Asia/Shanghai', connectTimeoutSeconds: 5,
    queryTimeoutSeconds: 300, maxOpenConnections: 10, maxIdleConnections: 2, prefetchRows: 1000, arraySize: 1000, enabled: true,
  }
  assert.match(validateDatasourceSave(input, false), /数据源编码/)
  input.code = 'report_oracle'
  assert.equal(validateDatasourceConnection(input, false), '创建数据源时必须填写密码。')
  assert.equal(validateDatasourceConnection(input, true), '')
})

test('Oracle datasource draft test sends connection fields without saving metadata', async () => {
  let captured = null
  const client = async (path, options) => {
    captured = { path, options }
    return { ok: true, status: 200, data: { data: { status: 'SUCCESS', testedAt: '2026-08-13T08:00:00Z', latencyMs: 18, message: 'Oracle 连接测试成功' } } }
  }
  const result = await testReportDatasourceConnection(client, {
    code: 'draft_oracle', name: '草稿连接', host: 'oracle.internal', port: 1521, serviceName: 'REPORT', sid: '',
    username: 'report_user', password: 'draft-password', sessionTimezone: 'Asia/Shanghai', connectTimeoutSeconds: 5,
    queryTimeoutSeconds: 300, maxOpenConnections: 10, maxIdleConnections: 2, prefetchRows: 1000, arraySize: 1000, enabled: true,
  }, 7)
  assert.equal(result.ok, true)
  assert.equal(captured.path, '/v1/report-datasource-connection-tests')
  assert.equal(captured.options.method, 'POST')
  assert.equal(captured.options.body.datasourceId, 7)
  assert.equal(captured.options.body.password, 'draft-password')
  assert.equal(Object.hasOwn(captured.options.body, 'code'), false)
  assert.equal(Object.hasOwn(captured.options.body, 'name'), false)
  assert.equal(Object.hasOwn(captured.options.body, 'enabled'), false)
})

test('Oracle datasource draft test reuses a saved password without sending an empty password', async () => {
  let captured = null
  const client = async (path, options) => {
    captured = { path, options }
    return { ok: true, status: 200, data: { data: { status: 'SUCCESS', testedAt: '2026-08-13T08:00:00Z', latencyMs: 18, message: 'Oracle 连接测试成功' } } }
  }
  await testReportDatasourceConnection(client, {
    code: 'saved_oracle', name: '已保存连接', host: 'oracle.internal', port: 1521, serviceName: 'REPORT', sid: '',
    username: 'report_user', password: '', sessionTimezone: 'Asia/Shanghai', connectTimeoutSeconds: 5,
    queryTimeoutSeconds: 300, maxOpenConnections: 10, maxIdleConnections: 2, prefetchRows: 1000, arraySize: 1000, enabled: true,
  }, 7)
  assert.equal(captured.options.body.datasourceId, 7)
  assert.equal(Object.hasOwn(captured.options.body, 'password'), false)
})

test('report audit parser preserves safe cursor records and structured detail', () => {
  const page = parseReportAuditPage({ data: {
    items: [{
      id: 88, actorType: 'USER', actorUserId: 7, action: 'REPORT_RUN_RESULT_READ', targetType: 'REPORT_RUN', targetId: 31,
      requestId: 'request-uuid', detail: { rowCount: 100, cursor: 'redacted' }, createdAt: '2026-08-13T08:00:00Z',
    }],
    hasMore: true,
    nextAfterId: 88,
  } })
  assert.equal(page.items[0].action, 'REPORT_RUN_RESULT_READ')
  assert.equal(page.items[0].actorType, 'USER')
  assert.deepEqual(page.items[0].detail, { rowCount: 100, cursor: 'redacted' })
  assert.equal(page.nextAfterId, 88)
})

test('report audit parser accepts a system actor and rejects inconsistent actors', () => {
  const base = { id: 89, action: 'REPORT_RUN_SUCCEEDED', targetType: 'REPORT_RUN', targetId: 31, requestId: 'system-request', detail: {}, createdAt: '2026-08-13T08:00:00Z' }
  const page = parseReportAuditPage({ data: { items: [{ ...base, actorType: 'SYSTEM', actorUserId: 0 }], hasMore: false } })
  assert.equal(page.items[0].actorType, 'SYSTEM')
  assert.equal(page.items[0].actorUserId, 0)
  assert.throws(() => parseReportAuditPage({ data: { items: [{ ...base, actorType: 'USER', actorUserId: 0 }], hasMore: false } }))
  assert.throws(() => parseReportAuditPage({ data: { items: [{ ...base, actorType: 'SYSTEM', actorUserId: 7 }], hasMore: false } }))
})

test('report audit parser rejects incomplete records and missing cursors', () => {
  assert.throws(() => parseReportAuditPage({ data: { items: [{ id: 1 }], hasMore: false } }))
  assert.throws(() => parseReportAuditPage({ data: { items: [], hasMore: true } }))
  assert.throws(() => parseReportAuditPage({ data: {} }))
  assert.throws(() => parseReportAuditPage({ data: { items: 'invalid', hasMore: false } }))
  assert.throws(() => parseReportAuditPage({ data: { items: [], hasMore: 'invalid' } }))
  assert.throws(() => parseReportAuditPage({ data: { items: [], hasMore: false, nextAfterId: 1 } }))
  const audit = { actorUserId: 7, action: 'REPORT_RESULT_QUERY_SUCCESS', targetType: 'REPORT_RUN', targetId: 31, requestId: 'request-uuid', detail: {}, createdAt: '2026-08-13T08:00:00Z' }
  assert.throws(() => parseReportAuditPage({ data: { items: [{ ...audit, id: 88 }], hasMore: true, nextAfterId: 99 } }))
  assert.throws(() => parseReportAuditPage({ data: { items: [{ ...audit, id: 88 }, { ...audit, id: 88 }], hasMore: true, nextAfterId: 88 } }))
  assert.throws(() => parseReportAuditPage({ data: { items: [{ ...audit, id: 87 }, { ...audit, id: 88 }], hasMore: true, nextAfterId: 88 } }))
})

test('report audit request builds bounded encoded cursor filters', async () => {
  let request
  const client = async (path, options) => {
    request = { path, options }
    return { ok: true, data: { data: { items: [], hasMore: false } } }
  }
  const result = await getReportAudits(client, {
    action: ' REPORT_RESULT_QUERY_SUCCESS ', targetType: 'REPORT_RUN', targetId: 31, afterId: 88, limit: 1000,
  })
  assert.equal(result.ok, true)
  assert.equal(request.path, '/v1/report-audits?action=REPORT_RESULT_QUERY_SUCCESS&targetType=REPORT_RUN&targetId=31&afterId=88&limit=100')
  assert.deepEqual(request.options, { method: 'GET', signal: undefined, showResult: false, silentLoading: true, acceptSafeErrorMessage: true })
})

test('SELECT snapshot draft keeps source identities and sends one editable column contract', async () => {
  const fieldId = '11111111-1111-4111-8111-111111111111'
  const querySpec = { version: 1, base: { tableId: '13019', aliasId: 'base' }, relations: [{ id: 'retail', name: '零售单', fromAliasId: 'base', targetTableId: '12964', keyBindings: [{ sourceColumnId: '31', targetColumnId: '41' }], cardinality: 'MANY_TO_ONE', joinType: 'LEFT', displayColumnIds: ['42'], detailColumnIds: ['42', '43'], matchConditions: { logic: 'AND', items: [] } }], columns: [{ fieldId, code: 'retail_ref', source: { kind: 'RELATION_DISPLAY', relationId: 'retail' }, header: '零售单', valueMode: 'DISPLAY', previewVisible: true, exportVisible: true }], where: { logic: 'OR', items: [{ aliasId: 'retail', columnId: '43', operator: 'GTE', value: { kind: 'PARAMETER', code: 'start' } }] }, orderBy: [{ aliasId: 'base', columnId: '30', direction: 'ASC' }], allowedFilters: [{ aliasId: 'retail', columnId: '43', operators: ['GTE', 'LT'] }], allowedSortFields: [{ aliasId: 'base', columnId: '30' }] }
  const payload = { data: { id: 9, code: 'query_report', name: '自定义查询', category: '销售', datasourceId: 3, lockVersion: 4, executionMode: 'SELECT_SNAPSHOT', inputSchema: { start: { type: 'str', displayName: '开始日期', control: 'DATE', required: false, format: 'YYYYMMDD' } }, querySpec, executionPolicy: { exportPolicy: 'MANUAL', resultRetentionHours: 24, purgeAfterExport: false }, columns: [{ fieldId, logicalCode: 'retail_ref', source: { kind: 'RELATION_DISPLAY', relationId: 'retail' }, previewHeader: '零售单', excelHeader: '零售单' }] } }
  const draft = parseReportDraft(payload)
  assert.equal(draft.executionMode, 'SELECT_SNAPSHOT')
  assert.deepEqual(draft.querySpec, querySpec)
  assert.equal(draft.columns[0].databaseColumn, '')
  assert.equal(draft.inputSchema.start.format, 'YYYYMMDD')
  let sent
  const response = await saveReportDraft(async (_path, options) => { sent = options.body; return { ok: true, data: payload } }, draft)
  assert.equal(response.ok, true)
  assert.deepEqual(sent.querySpec, querySpec)
  assert.equal(Object.hasOwn(sent, 'columns'), false)
  assert.equal(Object.hasOwn(sent, 'procedure'), false)
  assert.equal(sent.executionPolicy.purgeAfterExport, false)
  const detail = parseReportVersionDetail({ data: { summary: { id: 21, version: 2, status: 'PUBLISHED', contractFingerprint: 'aaaaaaaaaaaa', parameterCount: 1, columnCount: 1, grantCount: 0 }, configuration: payload.data } })
  assert.deepEqual(detail.configuration.querySpec, querySpec)
})

test('SELECT run contract and result preserve display values separately from raw keys', async () => {
  const contract = parseReportRunContract({ data: { definitionId: 9, versionId: 21, executionMode: 'SELECT_SNAPSHOT', inputSchema: {}, allowedFilters: [{ aliasId: 'retail', columnId: '43', operators: ['GTE'] }], allowedSortFields: [], relations: [], executionPolicy: { exportPolicy: 'MANUAL', resultRetentionHours: 24, purgeAfterExport: false } } })
  assert.equal(contract.jsonInput, true)
  assert.equal(contract.allowedFilters[0].columnId, '43')
  const run = { id: 31, definitionId: 9, versionId: 21, status: 'SUCCEEDED', resultAvailable: true }
  const result = parseReportResultPage({ data: { run, columns: [{ fieldId: 'uuid', code: 'retail_ref', header: '零售单', relationId: 'retail', valueMode: 'DISPLAY', capabilities: { canReadRaw: false, canViewDetail: true } }], rows: [{ key: 'signed-row', values: {}, displayValues: { retail_ref: '202610200001' }, relationStates: { retail: { status: 'MATCHED', canViewDetail: true } } }], pagination: { pageSize: 100, hasMore: false } } })
  assert.deepEqual(result.rows[0].values, {})
  assert.equal(result.rows[0].displayValues.retail_ref, '202610200001')
  assert.equal(result.columns[0].capabilities.canReadRaw, false)
  assert.equal(result.rows[0].relationStates.retail.canViewDetail, true)
  let sent
  const queryInputs = { where: { logic: 'AND', items: [] }, orderBy: [] }
  await createReportRun(async (_path, options) => { sent = options.body; return { ok: true, data: { data: run } } }, 9, { id: '000123' }, 'SELECT_SNAPSHOT', true, undefined, queryInputs)
  assert.deepEqual(sent, { conditions: { id: '000123' }, queryInputs })
})


test('manual exports settle when ready while legacy exports still wait for cleanup', () => {
  const ready = { status: 'READY', purgedAt: null }
  assert.equal(isReportExportSettled(ready), false)
  assert.equal(isReportExportSettled(ready, false), true)
  assert.equal(canStartNewReportRun('SUCCEEDED', ready, false, false), true)
  assert.equal(canStartNewReportRun('SUCCEEDED', ready, false), false)
  assert.equal(isReportExportSettled({ status: 'RUNNING', purgedAt: null }, false), false)
})

test('relation input bindings retain key text and reject competing option sources', () => {
  const schema = parseReportInputSchemaDocument({ product: { type: 'str', displayName: '商品', control: 'SELECT', required: false, relationId: 'rel_product', relationKeyColumnId: '123' } })
  assert.equal(schema.product.relationId, 'rel_product')
  assert.equal(schema.product.relationKeyColumnId, '123')
  assert.deepEqual(buildReportConditions(schema, { product: '00012345678901234567890' }), { ok: true, conditions: { product: '00012345678901234567890' } })
  assert.throws(() => parseReportInputSchemaDocument({ product: { ...schema.product, queryName: 'products' } }))
  assert.throws(() => parseReportInputSchemaDocument({ product: { ...schema.product, type: 'number' } }))
  assert.throws(() => parseReportInputSchemaDocument({ product: { ...schema.product, relationKeyColumnId: 'raw_field' } }))
})


test('SELECT publication summary does not require a procedure or physical result table', () => {
 const publication = parsePublication({ data: { definitionId: 9, versionId: 21, version: 2, contractHash: 'a'.repeat(64), validation: { executionMode: 'SELECT_SNAPSHOT', validatedAt: '2026-10-20T12:00:00Z', query: { baseTableId: '1', relationCount: 2, columnCount: 3, schemaHash: 'b'.repeat(64) }, executionPolicy: { exportPolicy: 'MANUAL', resultRetentionHours: 24, purgeAfterExport: false }, export: { exportableColumnCount: 3, schemaHash: 'c'.repeat(64) } } } })
 assert.equal(publication.validation.executionMode, 'SELECT_SNAPSHOT')
 assert.equal(publication.validation.query.relationCount, 2)
 assert.equal(publication.validation.executionPolicy.purgeAfterExport, false)
})

test('SELECT version comparison retains query and execution policy changes', () => {
 const version = (id) => ({ id, version: id, status: 'PUBLISHED', contractFingerprint: 'a'.repeat(12), parameterCount: 0, columnCount: 2, grantCount: 0 })
 const sections = [{ key: 'procedure', label: '存储过程', changes: [] }, { key: 'parameters', label: '{{形参}}', changes: [] }, { key: 'results', label: '结果字段与 Excel', changes: [] }, { key: 'excel', label: 'Excel 契约', changes: [] }, { key: 'permissions', label: '权限', changes: [] }, { key: 'query', label: '查询定义', changes: [{ kind: 'CHANGED', key: 'querySpec', label: '查询定义', before: 'b'.repeat(12), after: 'c'.repeat(12) }] }, { key: 'policy', label: '执行策略', changes: [{ kind: 'CHANGED', key: 'executionPolicy', label: '执行策略', before: 'd'.repeat(12), after: 'e'.repeat(12) }] }]
 const diff = parseReportVersionDiff({ data: { base: version(1), target: version(2), sections } })
 assert.equal(diff.sections.length, 7)
 assert.equal(diff.sections[5].changes[0].after, 'c'.repeat(12))
 assert.equal(diff.sections[6].changes[0].key, 'executionPolicy')
 assert.throws(() => parseReportVersionDiff({ data: { base: version(1), target: version(2), sections: sections.filter((section) => section.key !== 'permissions' && section.key !== 'excel') } }))
})


test('designers respect query and detail field restrictions while null inherits defaults', () => {
 const column = { id: 11, isPhysical: true, isSelectable: true }
 assert.equal(isBojunColumnAllowed({ config: { selectableColumns: [] } }, column), false)
 assert.equal(isBojunColumnAllowed({ config: { selectableColumns: ['11'], detailColumns: [] } }, column), true)
 assert.equal(isBojunColumnAllowed({ config: { selectableColumns: ['11'], detailColumns: [] } }, column, true), false)
 assert.equal(isBojunColumnAllowed({ config: { selectableColumns: null, detailColumns: null } }, column, true), true)
 assert.equal(isBojunColumnAllowed({ config: {} }, { ...column, isPhysical: false }), false)
})

test('direct runtime filters preserve exact numeric text and independent alias identities', () => {
  const field = { aliasId: 'store', columnId: '21', label: '门店编码', logicalType: 'integer', operators: ['EQ', 'BETWEEN', 'IN', 'IS_NULL'] }
  const other = { ...field, aliasId: 'warehouse' }
  const contract = { allowedFilters: [field, other], allowedSortFields: [] }
  const initial = { where: { logic: 'AND', items: [] }, orderBy: [] }
  const text = '000123456789012345678901234567890'
  const next = updateRuntimeFilter(initial, field, 'EQ', text)
  const both = updateRuntimeFilter(next, other, 'EQ', '002')
  assert.equal(readRuntimeFilter(both, field).value.value, text)
  assert.equal(both.where.items.length, 2)
  assert.equal(validateRuntimeQueryInputs(contract, both), '')
  assert.equal(isSimpleRuntimeWhere(contract, both), true)
  assert.equal(initial.where.items.length, 0)
  assert.throws(() => updateRuntimeFilter(initial, field, 'CONTAINS', 'x'), /不允许/)
})

test('blank runtime controls are omitted while sorting and filled values stay intact', () => {
  const field = { aliasId: 'base', columnId: '11', logicalType: 'string', operators: ['EQ', 'BETWEEN', 'IN'] }
  const contract = { allowedFilters: [field], allowedSortFields: [{ aliasId: 'base', columnId: '11' }] }
  const orderBy = [{ aliasId: 'base', columnId: '11', direction: 'DESC' }]
  for (const [operator, raw] of [['EQ', '   '], ['BETWEEN', ['', '']], ['IN', ['', ' ']]]) {
    const value = updateRuntimeFilter({ orderBy }, field, operator, raw)
    assert.equal(value.where.items.length, 1)
    assert.equal(validateRuntimeQueryInputs(contract, value), '')
    assert.deepEqual(normalizeRuntimeQueryInputs(value), { orderBy, where: { logic: 'AND', items: [] } })
    assert.equal(value.where.items.length, 1)
  }
  const cleared = updateRuntimeFilter(updateRuntimeFilter({ orderBy }, field, 'EQ', 'abc'), field, '')
  assert.deepEqual(cleared.where.items, [])
  assert.equal(cleared.orderBy, orderBy)
})

test('runtime ranges reject half-filled endpoints and invalid exact numbers with field labels', () => {
  const field = { aliasId: 'base', columnId: '12', label: '销售金额', logicalType: 'decimal', operators: ['BETWEEN', 'EQ'] }
  const contract = { allowedFilters: [field] }
  assert.match(validateRuntimeQueryInputs(contract, updateRuntimeFilter({}, field, 'BETWEEN', ['12.30', ''])), /销售金额.*完整填写区间/)
  assert.match(validateRuntimeQueryInputs(contract, updateRuntimeFilter({}, field, 'EQ', '1e20')), /有效数字/)
  const exact = ['12345678901234567890.12345678', '12345678901234567891.12345678']
  assert.equal(validateRuntimeQueryInputs(contract, updateRuntimeFilter({}, field, 'BETWEEN', exact)), '')
  assert.deepEqual(normalizeRuntimeQueryInputs(updateRuntimeFilter({}, field, 'BETWEEN', exact)).where.items[0].value.value, exact)
})

test('runtime collections discard empty separators and retain text keys without numeric conversion', () => {
  const field = { aliasId: 'base', columnId: '13', logicalType: 'integer', operators: ['IN', 'NOT_IN'] }
  const value = updateRuntimeFilter({}, field, 'IN', ['00123', '', ' 00012345678901234567890 ', ''])
  assert.equal(validateRuntimeQueryInputs({ allowedFilters: [field] }, value), '')
  assert.deepEqual(normalizeRuntimeQueryInputs(value).where.items[0].value.value, ['00123', '00012345678901234567890'])
  assert.match(validateRuntimeQueryInputs({ allowedFilters: [field] }, updateRuntimeFilter({}, field, 'IN', Array.from({ length: 101 }, () => '1'))), /1 至 100/)
})

test('runtime NULL operators carry no value and approved operators cannot be bypassed', () => {
  const field = { aliasId: 'base', columnId: '14', label: '备注', logicalType: 'string', operators: ['IS_NULL', 'EQ'] }
  const contract = { allowedFilters: [field] }
  const value = updateRuntimeFilter({}, field, 'IS_NULL')
  assert.equal(Object.hasOwn(value.where.items[0], 'value'), false)
  assert.equal(validateRuntimeQueryInputs(contract, value), '')
  assert.match(validateRuntimeQueryInputs(contract, { where: { logic: 'AND', items: [{ ...value.where.items[0], value: { kind: 'CONSTANT', value: 'x' } }] } }), /无需填写值/)
  assert.match(validateRuntimeQueryInputs(contract, { where: { logic: 'AND', items: [{ aliasId: 'base', columnId: '14', operator: 'CONTAINS', value: { kind: 'CONSTANT', value: 'x' } }] } }), /未获当前报表允许/)
})

test('native datetime controls round-trip the server accepted local date format', () => {
  assert.equal(runtimeDateValue('2026-10-22T11:30', 'datetime'), '2026-10-22 11:30:00')
  assert.equal(runtimeDateValue('2026-10-22T11:30:25', 'datetime'), '2026-10-22 11:30:25')
  assert.equal(runtimeDateInput('2026-10-22 11:30:00', 'datetime'), '2026-10-22T11:30:00')
  assert.equal(runtimeDateValue('', 'datetime'), '')
  assert.equal(runtimeDateValue('2026-10-22', 'date'), '2026-10-22')
  const field = { aliasId: 'base', columnId: '15', label: '创建时间', logicalType: 'datetime', operators: ['BETWEEN'] }
  assert.equal(validateRuntimeQueryInputs({ allowedFilters: [field] }, updateRuntimeFilter({}, field, 'BETWEEN', ['2026-10-22 00:00:00', '2026-10-22 23:59:59'])), '')
  assert.match(validateRuntimeQueryInputs({ allowedFilters: [field] }, updateRuntimeFilter({}, field, 'BETWEEN', ['2026-02-30 00:00:00', '2026-10-22 23:59:59'])), /有效日期/)
})

test('existing complex runtime conditions remain available for compatible editing', () => {
  const field = { aliasId: 'base', columnId: '16', operators: ['EQ'] }
  const leaf = { aliasId: 'base', columnId: '16', operator: 'EQ', value: { kind: 'CONSTANT', value: 'A' } }
  const contract = { allowedFilters: [field] }
  const value = { where: { logic: 'OR', items: [leaf, { ...leaf, value: { kind: 'CONSTANT', value: 'B' } }] } }
  assert.equal(isSimpleRuntimeWhere(contract, value), false)
  assert.equal(validateRuntimeQueryInputs(contract, value), '')
  assert.deepEqual(normalizeRuntimeQueryInputs(value), value)
  assert.equal(isSimpleRuntimeWhere(contract, { where: { logic: 'AND', items: [leaf, leaf] } }), false)
})

test('runtime validation enforces published sort permissions and bounded condition count', () => {
  const field = { aliasId: 'base', columnId: '17', operators: ['EQ'] }
  const contract = { allowedFilters: [field], allowedSortFields: [{ aliasId: 'base', columnId: '17' }] }
  assert.match(validateRuntimeQueryInputs(contract, { orderBy: [{ aliasId: 'other', columnId: '17', direction: 'ASC' }] }), /排序字段未获/)
  const leaf = { aliasId: 'base', columnId: '17', operator: 'EQ', value: { kind: 'CONSTANT', value: 'A' } }
  assert.match(validateRuntimeQueryInputs(contract, { where: { logic: 'AND', items: Array.from({ length: 21 }, () => leaf) } }), /最多设置 20/)
})

test('compact DATE conditions display in native controls without rewriting the submitted value', () => {
  const field = { aliasId: 'base', columnId: '51', logicalType: 'date', operators: ['EQ', 'BETWEEN', 'IN'] }
  const contract = { allowedFilters: [field] }
  assert.equal(runtimeDateInput('20261022', 'date'), '2026-10-22')
  assert.equal(runtimeDateInput(20261022, 'date'), '2026-10-22')
  const value = updateRuntimeFilter({}, field, 'BETWEEN', ['20261022', '2026-10-23'])
  assert.equal(isSimpleRuntimeWhere(contract, value), true)
  assert.deepEqual(normalizeRuntimeQueryInputs(value).where.items[0].value.value, ['20261022', '2026-10-23'])
  assert.equal(validateRuntimeQueryInputs(contract, value), '')
})

test('date values carrying time and zoned or precise timestamps retain compatibility editing without truncation', () => {
  const fields = [
    { aliasId: 'base', columnId: '52', logicalType: 'date', operators: ['EQ', 'BETWEEN', 'IN'] },
    { aliasId: 'base', columnId: '53', logicalType: 'datetime', operators: ['EQ', 'BETWEEN', 'IN'] },
  ]
  const cases = [
    [fields[0], '2026-10-22 12:34:56'],
    [fields[0], '20261022123456'],
    [fields[1], '2026-10-22T12:34:56Z'],
    [fields[1], '2026-10-22T12:34:56+08:00'],
    [fields[1], '2026-10-22T12:34:56.123456789+08:00'],
    [fields[1], '2026-10-22 12:34:56.123456789'],
    [fields[1], '2026-10-22'],
    [fields[1], '20261022123456'],
  ]
  for (const [field, raw] of cases) {
    const value = updateRuntimeFilter({}, field, 'EQ', raw)
    assert.equal(isSimpleRuntimeWhere({ allowedFilters: fields }, value), false, `${field.logicalType}: ${raw}`)
    assert.equal(normalizeRuntimeQueryInputs(value).where.items[0].value.value, raw)
    assert.equal(validateRuntimeQueryInputs({ allowedFilters: fields }, value), '', raw)
    const range = updateRuntimeFilter({}, field, 'BETWEEN', [raw, ''])
    assert.equal(isSimpleRuntimeWhere({ allowedFilters: fields }, range), false)
    const collection = updateRuntimeFilter({}, field, 'IN', [raw])
    assert.equal(isSimpleRuntimeWhere({ allowedFilters: fields }, collection), true)
  }
  assert.equal(runtimeDateInput('2026-10-22T12:34:56.123456789+08:00', 'datetime'), '2026-10-22T12:34:56.123456789+08:00')
  assert.equal(isSimpleRuntimeWhere({ allowedFilters: fields }, updateRuntimeFilter({}, fields[1], 'BETWEEN', ['2026-10-22 00:00:00', '2026-10-23 23:59:59'])), true)
})
