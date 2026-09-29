import assert from 'node:assert/strict'
import test from 'node:test'

import {
  bojunImportWriteConfirmation,
  bojunImportWriteFieldOptions,
  buildExcelExportConfig,
  cloneExcelEmptyCellFills,
  cloneExcelImportWriteMappings,
  excelMatchSchemePath,
  excelFieldSelectOptions,
  excelModelSelectOptions,
  isExcelMatchStepComplete,
  migrateExcelMatchSteps,
  migrateExcelImportWriteMappings,
  selectExcelMatchStepModel,
} from '../.test-dist/excelMatchConfig.js'

test('Bojun Excel import exposes Oracle backfill fields with safe confirmation copy', () => {
  assert.deepEqual(
    bojunImportWriteFieldOptions.slice(2).map((option) => option.value),
    ['order_phone', 'paid_amount', 'push_amount', 'is_to_shop', 'oracle_retail_id'],
  )
  assert.match(bojunImportWriteConfirmation('oracle_retail_id'), /正整数/)
  assert.match(bojunImportWriteConfirmation(['is_to_shop', 'oracle_retail_id']), /按列填充/)
  assert.match(bojunImportWriteConfirmation(['is_to_shop', 'oracle_retail_id']), /不会覆盖已有值/)
  assert.match(bojunImportWriteConfirmation('paid_amount'), /当前为 0/)
  assert.match(bojunImportWriteConfirmation('is_to_shop'), /Y 或 N/)
})

test('migrateExcelImportWriteMappings upgrades a legacy single-column scheme', () => {
  const mappings = migrateExcelImportWriteMappings({
    dbWriteField: 'paid_amount',
    writeExcelColumn: '实付金额',
  }, { dbWriteField: 'matched_docno', writeExcelColumn: '订单号' })

  assert.deepEqual(mappings, [{ dbWriteField: 'paid_amount', writeExcelColumn: '实付金额' }])
})

test('migrateExcelImportWriteMappings preserves and clones multiple mappings', () => {
  const source = {
    writeMappings: [
      { dbWriteField: 'order_phone', writeExcelColumn: '订单手机号' },
      { dbWriteField: 'paid_amount', writeExcelColumn: '实付金额' },
    ],
  }
  const mappings = migrateExcelImportWriteMappings(source, { dbWriteField: 'matched_docno', writeExcelColumn: '订单号' })
  mappings[0].writeExcelColumn = '已修改'

  assert.equal(source.writeMappings[0].writeExcelColumn, '订单手机号')
  assert.equal(mappings[1].dbWriteField, 'paid_amount')
  assert.notEqual(mappings, source.writeMappings)
})

test('cloneExcelImportWriteMappings returns independent mapping objects', () => {
  const source = [{ dbWriteField: 'push_amount', writeExcelColumn: '推送金额' }]
  const cloned = cloneExcelImportWriteMappings(source)
  cloned[0].dbWriteField = 'paid_amount'
  assert.equal(source[0].dbWriteField, 'push_amount')
})

test('excelMatchSchemePath only builds a positive scheme resource path', () => {
  assert.equal(excelMatchSchemePath(42), '/v1/excel-match-jobs/schemes/42')
  assert.throws(() => excelMatchSchemePath(0), RangeError)
  assert.throws(() => excelMatchSchemePath(1.5), RangeError)
})

const fallbackStep = {
  name: '匹配伯俊门店',
  filters: [],
  matchMode: 'field',
  tableName: 'bojun_retail_orders',
  matchExcelColumn: '原始线上订单号',
  dbMatchField: 'matched_docno',
  dbValueField: 'c_store_name',
  outputColumnName: '线下店名称',
  conditionOp: 'contains',
  containsValue: '',
  writeValue: '',
  specExcelColumn: '',
  priceExcelColumn: '',
  qtyExcelColumn: '',
}

test('migrateExcelMatchSteps moves legacy top-level filters to the first step only', () => {
  const source = {
    filters: [{ column: '类型', op: 'eq', value: 'A' }],
    steps: [
      { ...fallbackStep, name: '步骤 1' },
      { ...fallbackStep, name: '步骤 2', tableName: 'other_orders', outputColumnName: '第二步结果' },
    ],
  }

  const steps = migrateExcelMatchSteps(source, fallbackStep)

  assert.deepEqual(steps[0].filters, [{ column: '类型', op: 'eq', value: 'A' }])
  assert.deepEqual(steps[1].filters, [])
  assert.deepEqual(source.steps[0].filters, [])
})

test('migrateExcelMatchSteps preserves independent filters on every step', () => {
  const steps = migrateExcelMatchSteps({
    steps: [
      { ...fallbackStep, filters: [{ column: '类型', op: 'eq', value: 'A' }] },
      { ...fallbackStep, filters: [{ column: '类型', op: 'eq', value: 'B' }], outputColumnName: 'B结果' },
    ],
  }, fallbackStep)

  assert.deepEqual(steps.map((step) => step.filters), [
    [{ column: '类型', op: 'eq', value: 'A' }],
    [{ column: '类型', op: 'eq', value: 'B' }],
  ])
})

test('migrateExcelMatchSteps defaults legacy schemes and steps to field mode', () => {
  const legacyTopLevel = migrateExcelMatchSteps({
    tableName: 'bojun_retail_orders',
    matchExcelColumn: '订单号',
    dbMatchField: 'docno',
    dbValueField: 'items_json',
    outputColumnName: 'SKU',
  }, fallbackStep)
  const legacyStep = migrateExcelMatchSteps({
    steps: [{ ...fallbackStep, matchMode: undefined }],
  }, fallbackStep)

  assert.equal(legacyTopLevel[0].matchMode, 'field')
  assert.equal(legacyStep[0].matchMode, 'field')
  assert.equal(legacyStep[0].specExcelColumn, '')
  assert.equal(legacyTopLevel[0].conditionOp, 'contains')
  assert.equal(legacyTopLevel[0].containsValue, '')
  assert.equal(legacyTopLevel[0].writeValue, '')
  assert.equal(legacyStep[0].containsValue, '')
  assert.equal(legacyStep[0].writeValue, '')
})

test('migrateExcelMatchSteps preserves conditional write rules and defaults new fields', () => {
  const steps = migrateExcelMatchSteps({
    steps: [{
      name: '标记渠道',
      matchMode: 'conditional_write',
      matchExcelColumn: '店铺',
      containsValue: '有赞',
      outputColumnName: '渠道',
      writeValue: ' 线上 ',
    }, {
      name: '旧步骤',
      matchExcelColumn: '订单号',
    }],
  }, fallbackStep)

  assert.equal(steps[0].matchMode, 'conditional_write')
  assert.equal(steps[0].conditionOp, 'contains')
  assert.equal(steps[0].containsValue, '有赞')
  assert.equal(steps[0].writeValue, ' 线上 ')
  assert.equal(steps[0].tableName, '')
  assert.equal(steps[1].containsValue, '')
  assert.equal(steps[1].writeValue, '')
})

test('migrateExcelMatchSteps preserves equality and inequality conditions including empty comparison values', () => {
  const steps = migrateExcelMatchSteps({
    steps: ['eq', 'neq'].map((conditionOp) => ({
      ...fallbackStep,
      matchMode: 'conditional_write',
      conditionOp,
      containsValue: '',
      writeValue: ' 标记 ',
    })),
  }, fallbackStep)

  assert.deepEqual(steps.map((step) => step.conditionOp), ['eq', 'neq'])
  assert.deepEqual(steps.map((step) => step.containsValue), ['', ''])
  assert.deepEqual(steps.map((step) => step.writeValue), [' 标记 ', ' 标记 '])
})

test('conditional write completeness requires its condition and target but permits clearing without database fields', () => {
  const step = {
    ...fallbackStep,
    matchMode: 'conditional_write',
    tableName: '',
    dbMatchField: '',
    dbValueField: '',
    containsValue: '有赞',
    writeValue: '',
  }

  assert.equal(isExcelMatchStepComplete(step), true)
  assert.equal(isExcelMatchStepComplete({ ...step, containsValue: ' ' }), false)
  assert.equal(isExcelMatchStepComplete({ ...step, matchExcelColumn: '' }), false)
  assert.equal(isExcelMatchStepComplete({ ...step, outputColumnName: '' }), false)
  assert.equal(isExcelMatchStepComplete({ ...step, matchMode: 'field' }), false)
  assert.equal(isExcelMatchStepComplete(fallbackStep), true)
  assert.equal(isExcelMatchStepComplete({ ...fallbackStep, matchMode: 'order_item_sku' }), false)
  assert.equal(isExcelMatchStepComplete({ ...fallbackStep, matchMode: 'order_item_sku', specExcelColumn: '规格', priceExcelColumn: '金额', qtyExcelColumn: '数量' }), true)
})

test('conditional write completeness allows empty equality comparisons but still requires source and target columns', () => {
  const step = {
    ...fallbackStep,
    matchMode: 'conditional_write',
    tableName: '',
    dbMatchField: '',
    dbValueField: '',
    containsValue: '',
    writeValue: '',
  }

  for (const conditionOp of ['eq', 'neq']) {
    assert.equal(isExcelMatchStepComplete({ ...step, conditionOp }), true)
    assert.equal(isExcelMatchStepComplete({ ...step, conditionOp, containsValue: ' ' }), true)
    assert.equal(isExcelMatchStepComplete({ ...step, conditionOp, matchExcelColumn: '' }), false)
    assert.equal(isExcelMatchStepComplete({ ...step, conditionOp, outputColumnName: '' }), false)
  }
  assert.equal(isExcelMatchStepComplete({ ...step, conditionOp: 'contains' }), false)
  assert.equal(isExcelMatchStepComplete({ ...step, conditionOp: undefined }), false)
})

test('migrateExcelMatchSteps preserves order item SKU columns', () => {
  const [step] = migrateExcelMatchSteps({
    steps: [{
      ...fallbackStep,
      matchMode: 'order_item_sku',
      specExcelColumn: '规格编码',
      priceExcelColumn: '销售价格',
      qtyExcelColumn: '销售数量',
    }],
  }, fallbackStep)

  assert.equal(step.matchMode, 'order_item_sku')
  assert.equal(step.specExcelColumn, '规格编码')
  assert.equal(step.priceExcelColumn, '销售价格')
  assert.equal(step.qtyExcelColumn, '销售数量')
})

test('buildExcelExportConfig emits step filters without a top-level filter', () => {
  const config = buildExcelExportConfig({
    sheetName: ' Sheet1 ',
    steps: [{
      ...fallbackStep,
      name: ' 第一步 ',
      filters: [
        { column: ' 类型 ', op: ' contains ', value: ' A ' },
        { column: ' 备注 ', op: ' empty ', value: '旧值' },
        { column: ' ', op: 'eq', value: '' },
      ],
    }],
    emptyCellFills: [],
    exportColumnFormats: [],
    batchSize: 1000,
  })

  assert.equal(Object.hasOwn(config, 'filters'), false)
  assert.deepEqual(config.steps[0].filters, [
    { column: '类型', op: 'contains', value: 'A' },
    { column: '备注', op: 'empty', value: '' },
  ])
  assert.equal(config.steps[0].name, '第一步')
  assert.equal(config.steps[0].matchMode, 'field')
  assert.equal(config.steps[0].specExcelColumn, '')
})

test('buildExcelExportConfig emits trimmed order item SKU matching fields', () => {
  const config = buildExcelExportConfig({
    sheetName: 'Sheet1',
    steps: [{
      ...fallbackStep,
      matchMode: 'order_item_sku',
      matchExcelColumn: ' 订单号 ',
      dbMatchField: ' docno ',
      dbValueField: ' items_json ',
      outputColumnName: ' SKU ',
      specExcelColumn: ' 规格编码 ',
      priceExcelColumn: ' 销售价格 ',
      qtyExcelColumn: ' 销售数量 ',
    }],
    emptyCellFills: [],
    exportColumnFormats: [],
    batchSize: 1000,
  })

  assert.deepEqual(config.steps[0], {
    name: '匹配伯俊门店',
    matchMode: 'order_item_sku',
    filters: [],
    tableName: 'bojun_retail_orders',
    matchExcelColumn: '订单号',
    dbMatchField: 'docno',
    dbValueField: 'items_json',
    outputColumnName: 'SKU',
    conditionOp: 'contains',
    containsValue: '',
    writeValue: '',
    specExcelColumn: '规格编码',
    priceExcelColumn: '销售价格',
    qtyExcelColumn: '销售数量',
  })
})

test('buildExcelExportConfig preserves sequential conditional writes, whitespace and an empty write value', () => {
  const step = {
    ...fallbackStep,
    matchMode: 'conditional_write',
    tableName: '',
    dbMatchField: '',
    dbValueField: '',
    matchExcelColumn: ' 店铺 ',
    containsValue: ' 有赞 ',
    outputColumnName: ' 渠道 ',
    writeValue: ' 线上 ',
    filters: [{ column: '订单类型', op: 'eq', value: '零售' }],
  }
  const config = buildExcelExportConfig({
    sheetName: 'Sheet1',
    steps: [step, { ...step, name: '清空渠道', writeValue: '' }],
    emptyCellFills: [],
    exportColumnFormats: [],
    batchSize: 1000,
  })

  assert.equal(config.steps[0].matchMode, 'conditional_write')
  assert.equal(config.steps[0].conditionOp, 'contains')
  assert.equal(config.steps[0].matchExcelColumn, '店铺')
  assert.equal(config.steps[0].containsValue, '有赞')
  assert.equal(config.steps[0].outputColumnName, '渠道')
  assert.equal(config.steps[0].writeValue, ' 线上 ')
  assert.deepEqual(config.steps[0].filters, [{ column: '订单类型', op: 'eq', value: '零售' }])
  assert.equal(config.steps[1].name, '清空渠道')
  assert.equal(config.steps[1].outputColumnName, '渠道')
  assert.equal(config.steps[1].writeValue, '')
})

test('buildExcelExportConfig keeps comparison operators and text values while preserving literal writes', () => {
  const config = buildExcelExportConfig({
    sheetName: 'Sheet1',
    steps: [
      { ...fallbackStep, matchMode: 'conditional_write', conditionOp: 'eq', containsValue: ' 001 ', writeValue: ' 等于 ' },
      { ...fallbackStep, matchMode: 'conditional_write', conditionOp: 'neq', containsValue: ' ', writeValue: '   ' },
      { ...fallbackStep, matchMode: 'conditional_write', conditionOp: 'eq', containsValue: '', writeValue: '' },
      { ...fallbackStep, matchMode: 'conditional_write', conditionOp: undefined, containsValue: ' 有赞 ' },
    ],
    emptyCellFills: [],
    exportColumnFormats: [],
    batchSize: 1000,
  })

  assert.deepEqual(config.steps.map((step) => step.conditionOp), ['eq', 'neq', 'eq', 'contains'])
  assert.deepEqual(config.steps.map((step) => step.containsValue), ['001', '', '', '有赞'])
  assert.deepEqual(config.steps.map((step) => step.writeValue), [' 等于 ', '   ', '', ''])
})

test('buildExcelExportConfig trims and keeps empty cell fill rules', () => {
  const config = buildExcelExportConfig({
    sheetName: 'Sheet1',
    steps: [fallbackStep],
    emptyCellFills: [
      { targetColumn: ' 订单号 ', sourceColumn: ' 原订单号 ' },
      { targetColumn: '', sourceColumn: '' },
    ],
    exportColumnFormats: [],
    batchSize: 1000,
  })

  assert.deepEqual(config.emptyCellFills, [{ targetColumn: '订单号', sourceColumn: '原订单号' }])
})

test('cloneExcelEmptyCellFills creates independent defaults for legacy schemes', () => {
  const source = [{ targetColumn: '订单号', sourceColumn: '原订单号' }]
  const cloned = cloneExcelEmptyCellFills(source)

  cloned[0].targetColumn = '新订单号'
  assert.deepEqual(source, [{ targetColumn: '订单号', sourceColumn: '原订单号' }])
  assert.deepEqual(cloneExcelEmptyCellFills(undefined), [])
})

const modelCatalog = [{
  name: '伯俊零售订单',
  modelName: 'BojunRetailOrder',
  tableName: 'bojun_retail_orders',
  description: '伯俊零售单头数据',
  mapping: '伯俊零售订单（BojunRetailOrder） → 数据库表 bojun_retail_orders',
  fields: [
    {
      name: '伯俊零售单号',
      modelField: 'DocNo',
      columnName: 'docno',
      dataType: 'varchar(255)',
      description: '伯俊系统生成的零售单号',
      mapping: 'BojunRetailOrder.DocNo → bojun_retail_orders.docno',
      nullable: false,
    },
    {
      name: '伯俊门店名称',
      modelField: 'StoreName',
      columnName: 'c_store_name',
      dataType: 'varchar(255)',
      description: '伯俊订单所属门店名称',
      mapping: 'BojunRetailOrder.StoreName → bojun_retail_orders.c_store_name',
      nullable: true,
    },
    {
      name: '订单完成时间',
      modelField: 'CompletedAt',
      columnName: 'completed_at',
      dataType: 'datetime',
      description: '伯俊返回的订单完成时间',
      mapping: 'BojunRetailOrder.CompletedAt → bojun_retail_orders.completed_at',
      nullable: true,
    },
  ],
}, {
  name: '企迈订单',
  modelName: 'QIMAI_ORDER_DATA',
  tableName: 'qimai_order_data',
  description: '企迈渠道订单数据',
  mapping: '企迈订单（QIMAI_ORDER_DATA） → 数据库表 qimai_order_data',
  fields: [{
    name: '业务订单号',
    modelField: 'OrderNo',
    columnName: 'order_no',
    dataType: 'varchar(100)',
    description: '企迈业务订单号',
    mapping: 'QIMAI_ORDER_DATA.OrderNo → qimai_order_data.order_no',
    nullable: false,
  }],
}]

test('model and field option labels explain model-to-database mappings', () => {
  const modelOptions = excelModelSelectOptions(modelCatalog, '')
  const fieldOptions = excelFieldSelectOptions(modelCatalog[0].fields, '')

  assert.equal(modelOptions[0].label, '伯俊零售订单（BojunRetailOrder → bojun_retail_orders）')
  assert.equal(fieldOptions[0].label, '伯俊零售单号（DocNo → docno）')
  assert.equal(fieldOptions[2].label, '订单完成时间（CompletedAt → completed_at）')
})

test('selectExcelMatchStepModel clears fields missing from the selected model', () => {
  const step = {
    ...fallbackStep,
    dbMatchField: 'docno',
    dbValueField: 'c_store_name',
  }

  const switched = selectExcelMatchStepModel(step, 'qimai_order_data', modelCatalog)

  assert.equal(switched.tableName, 'qimai_order_data')
  assert.equal(switched.dbMatchField, '')
  assert.equal(switched.dbValueField, '')
})

test('catalog selectors preserve missing legacy table and field values as current options', () => {
  const modelOptions = excelModelSelectOptions(modelCatalog, 'legacy_orders')
  const fieldOptions = excelFieldSelectOptions(modelCatalog[0].fields, 'legacy_field')

  assert.deepEqual(modelOptions[0], {
    value: 'legacy_orders',
    label: '当前配置：legacy_orders（目录中不存在）',
    currentOnly: true,
  })
  assert.deepEqual(fieldOptions[0], {
    value: 'legacy_field',
    label: '当前配置：legacy_field（模型中不存在）',
    currentOnly: true,
  })
})
