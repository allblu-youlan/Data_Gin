import assert from 'node:assert/strict'
import test from 'node:test'

import {
  createMallWeatherChartCsv,
  createMallWeatherCsvZip,
  createMallWeatherDatasetCsv,
  downloadMallWeatherBytes,
  mallWeatherCsvEntryNames,
  mallWeatherCsvFileName,
  mallWeatherCsvKinds,
  mallWeatherCsvZipFileName,
  mallWeatherChartCsvFileName,
} from '../.test-dist/mallWeatherCsv.js'

const decoder = new TextDecoder()
const mall = { mallCode: 'SH-001', mallName: '上海测试商场' }

function decodeCsv(bytes) {
  assert.deepEqual(Array.from(bytes.slice(0, 3)), [0xef, 0xbb, 0xbf])
  return decoder.decode(bytes.slice(3))
}

function crc32(data) {
  let value = 0xffffffff
  for (const byte of data) {
    value ^= byte
    for (let bit = 0; bit < 8; bit++) {
      value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1
    }
  }
  return (value ^ 0xffffffff) >>> 0
}

function readStoredZip(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const entries = new Map()
  const localEntries = new Map()
  let offset = 0
  while (view.getUint32(offset, true) === 0x04034b50) {
    const localOffset = offset
    const flags = view.getUint16(offset + 6, true)
    const method = view.getUint16(offset + 8, true)
    const expectedCrc = view.getUint32(offset + 14, true)
    const compressedSize = view.getUint32(offset + 18, true)
    const uncompressedSize = view.getUint32(offset + 22, true)
    const nameLength = view.getUint16(offset + 26, true)
    const extraLength = view.getUint16(offset + 28, true)
    const nameStart = offset + 30
    const dataStart = nameStart + nameLength + extraLength
    const name = decoder.decode(bytes.slice(nameStart, nameStart + nameLength))
    const data = bytes.slice(dataStart, dataStart + compressedSize)
    assert.equal(flags & 0x0800, 0x0800)
    assert.equal(method, 0)
    assert.equal(compressedSize, uncompressedSize)
    assert.equal(crc32(data), expectedCrc)
    assert.equal(entries.has(name), false)
    entries.set(name, data)
    localEntries.set(name, {
      crc: expectedCrc,
      compressedSize,
      uncompressedSize,
      offset: localOffset,
    })
    offset = dataStart + compressedSize
  }
  assert.equal(view.getUint32(offset, true), 0x02014b50)
  const endOffset = bytes.byteLength - 22
  assert.equal(view.getUint32(endOffset, true), 0x06054b50)
  const centralOffset = view.getUint32(endOffset + 16, true)
  const centralSize = view.getUint32(endOffset + 12, true)
  assert.equal(view.getUint16(endOffset + 8, true), entries.size)
  assert.equal(view.getUint16(endOffset + 10, true), entries.size)
  assert.equal(centralOffset, offset)
  assert.equal(centralOffset + centralSize, endOffset)

  const centralNames = []
  while (offset < endOffset) {
    assert.equal(view.getUint32(offset, true), 0x02014b50)
    const flags = view.getUint16(offset + 8, true)
    const method = view.getUint16(offset + 10, true)
    const expectedCrc = view.getUint32(offset + 16, true)
    const compressedSize = view.getUint32(offset + 20, true)
    const uncompressedSize = view.getUint32(offset + 24, true)
    const nameLength = view.getUint16(offset + 28, true)
    const extraLength = view.getUint16(offset + 30, true)
    const commentLength = view.getUint16(offset + 32, true)
    const localOffset = view.getUint32(offset + 42, true)
    const name = decoder.decode(bytes.slice(offset + 46, offset + 46 + nameLength))
    const local = localEntries.get(name)
    assert.ok(local)
    assert.equal(flags & 0x0800, 0x0800)
    assert.equal(method, 0)
    assert.equal(expectedCrc, local.crc)
    assert.equal(compressedSize, local.compressedSize)
    assert.equal(uncompressedSize, local.uncompressedSize)
    assert.equal(localOffset, local.offset)
    centralNames.push(name)
    offset += 46 + nameLength + extraLength + commentLength
  }
  assert.deepEqual(centralNames, [...entries.keys()])
  return entries
}

test('creates Chinese mall-scoped CSV with BOM, CRLF and stable values', () => {
  const bytes = createMallWeatherDatasetCsv('realtime', [{
    snapshotAtLocal: '2026-07-28 10:30:00',
    providerServerTimeLocal: '2026-07-28 10:29:59',
    fetchedAtLocal: '2026-07-28 10:30:01',
    temperatureC: 26.5,
    humidityPct: 0,
    qualityStatus: 'GOOD',
    qualityWarnings: [{ code: 'W1', path: 'temperature' }],
  }], mall)
  const text = decodeCsv(bytes)
  const lines = text.split('\r\n')
  assert.equal(lines[0].startsWith('商场编码,商场名称,实况时间,供应商时间,采集时间,温度（℃）'), true)
  assert.equal(lines[1].startsWith('SH-001,上海测试商场,2026-07-28 10:30:00'), true)
  assert.match(lines[1], /,26\.5,/)
  assert.match(lines[1], /,0,/)
  assert.match(lines[1], /"\[\{""code"":""W1"",""path"":""temperature""\}\]"$/)
  assert.equal(lines.at(-1), '')
  assert.equal(text.replaceAll('\r\n', '').includes('\n'), false)
})

test('creates chart-scoped CSV aligned by time with only visible series', () => {
  const text = decodeCsv(createMallWeatherChartCsv([
    { id: 'temperature', name: '温度', unit: '°C', data: [{ time: '2026-08-02 10:00', value: 28 }, { time: '2026-08-02 11:00', value: 29 }] },
    { id: 'apparent', name: '体感温度', unit: '°C', data: [{ time: '2026-08-02 11:00', value: 31 }] },
  ], mall))
  assert.equal(text, [
    '商场编码,商场名称,时间,温度（°C）,体感温度（°C）',
    'SH-001,上海测试商场,2026-08-02 10:00:00,28,',
    'SH-001,上海测试商场,2026-08-02 11:00:00,29,31',
    '',
  ].join('\r\n'))
  assert.equal(mallWeatherChartCsvFileName('hourly_temperature', ' SH/001 '), 'SH_001_hourly_temperature.csv')
  assert.throws(() => mallWeatherChartCsvFileName('../bad', 'SH-001'), /invalid mall weather chart id/)
})

test('exports all 34 official life-index series and limits output by total cells', () => {
  const lifeSeries = Array.from({ length: 34 }, (_, index) => ({
    id: `life_${index + 1}`,
    name: `生活指数${index + 1}`,
    unit: '级',
    data: [{ time: '2026-08-02', value: index + 1 }],
  }))
  const text = decodeCsv(createMallWeatherChartCsv(lifeSeries, mall))
  const lines = text.split('\r\n')
  assert.equal(lines[0].split(',').length, 37)
  assert.equal(lines[1].split(',').length, 37)
  assert.match(lines[0], /生活指数34（级）$/)
  assert.match(lines[1], /,34$/)

  const oversized = Array.from({ length: 500 }, (_, index) => ({
    id: `series_${index}`,
    name: `序列${index}`,
    unit: '',
    data: Array.from({ length: 500 }, (_, pointIndex) => ({ time: String(pointIndex), value: pointIndex })),
  }))
  assert.throws(() => createMallWeatherChartCsv(oversized, mall), /too large/)
})

test('applies RFC4180 escaping, empty values and spreadsheet formula protection', () => {
  const text = decodeCsv(createMallWeatherDatasetCsv('alerts', [{
    alertId: '=SUM(A1:A2)',
    status: '+ACTIVE',
    title: '暴雨,"红色"\r\n预警',
    description: '-danger',
    code: '@cmd',
    alertTypeName: '\t测试',
    alertLevelName: '\r测试',
    source: '\n测试',
    location: '  =HYPERLINK("https://example.invalid")',
    qualityStatus: 'GOOD',
    qualityWarnings: [],
  }], mall))
  assert.match(text, /'\=SUM\(A1:A2\)/)
  assert.match(text, /'\+ACTIVE/)
  assert.match(text, /"暴雨,""红色""\r\n预警"/)
  assert.match(text, /'\-danger/)
  assert.match(text, /'@cmd/)
  assert.match(text, /'\t测试/)
  assert.match(text, /"'\r测试"/)
  assert.match(text, /"'\n测试"/)
  assert.match(text, /"'  =HYPERLINK\(""https:\/\/example\.invalid""\)"/)
  assert.match(text, /,,,/)
})

test('protects mall ownership columns from spreadsheet formula injection', () => {
  const weatherRows = [{
    alertId: 'A1', status: 'ACTIVE', title: '测试', qualityStatus: 'GOOD', qualityWarnings: [],
  }]
  const formulaRow = decodeCsv(createMallWeatherDatasetCsv('alerts', weatherRows, {
    mallCode: '=MALL',
    mallName: '+测试商场',
  })).split('\r\n')[1]
  const lineBreakRow = decodeCsv(createMallWeatherDatasetCsv('alerts', weatherRows, {
    mallCode: '=MALL',
    mallName: '测试\n商场',
  })).split('\r\n')[1]
  assert.equal(formulaRow.startsWith("'=MALL,'+测试商场,"), true)
  assert.equal(lineBreakRow.startsWith("'=MALL,\"测试\n商场\","), true)
})

test('creates header-only CSV for an empty dataset', () => {
  const text = decodeCsv(createMallWeatherDatasetCsv('life_indices', [], mall))
  assert.equal(text.split('\r\n').length, 2)
  assert.equal(text.startsWith('商场编码,商场名称,来源接口,预报日期,指数类型'), true)
})

test('defines Chinese headers for every field in all six datasets', () => {
  for (const kind of mallWeatherCsvKinds) {
    const header = decodeCsv(createMallWeatherDatasetCsv(kind, [], mall)).split('\r\n')[0]
    const columns = header.split(',')
    assert.deepEqual(columns.slice(0, 2), ['商场编码', '商场名称'])
    assert.equal(columns.length > 2, true)
    for (const column of columns) assert.match(column, /\p{Script=Han}/u)
  }
})

test('builds deterministic stored ZIP with exactly six safe fixed entries and valid CRC32', () => {
  const data = {
    minutely: [{
      forecastMinuteUtc: '2026-07-28T02:31:00Z',
      forecastMinuteLocal: '2026-07-28 10:31:00',
      issuedAtUtc: '2026-07-28T02:30:00Z',
      issuedAtLocal: '2026-07-28 10:30:00',
      fetchedAtUtc: '2026-07-28T02:30:01Z',
      fetchedAtLocal: '2026-07-28 10:30:01',
      minuteOffset: 1,
      precipitationMmH: 0.2,
      qualityStatus: 'GOOD',
      qualityWarnings: [],
    }],
  }
  const first = createMallWeatherCsvZip(data, mall)
  const second = createMallWeatherCsvZip(data, mall)
  assert.deepEqual(first, second)
  const entries = readStoredZip(first)
  assert.deepEqual([...entries.keys()], mallWeatherCsvKinds.map((kind) => mallWeatherCsvEntryNames[kind]))
  assert.equal(entries.size, 6)
  for (const [name, bytes] of entries) {
    assert.match(name, /^[a-z0-9_]+\.csv$/)
    const text = decodeCsv(bytes)
    assert.equal(text.startsWith('商场编码,商场名称,'), true)
  }
  assert.equal(decodeCsv(entries.get('realtime.csv')).split('\r\n').length, 2)
  assert.equal(decodeCsv(entries.get('minutely.csv')).split('\r\n').length, 3)
})

test('generates safe mall-code filenames without path traversal', () => {
  assert.equal(mallWeatherCsvFileName('hourly', 'SH-001'), 'SH-001_hourly.csv')
  assert.equal(mallWeatherCsvZipFileName('SH-001'), 'SH-001_weather_csv.zip')
  assert.equal(mallWeatherCsvFileName('alerts', '../../CON:?*'), '_CON_alerts.csv')
  assert.equal(mallWeatherCsvZipFileName('  '), 'mall_weather_csv.zip')
  assert.doesNotMatch(mallWeatherCsvFileName('daily', '../mall'), /\.\.|\//)
  assert.throws(() => mallWeatherCsvFileName('unknown', 'SH-001'), /invalid mall weather CSV kind/)
})

test('downloads only safe CSV or ZIP names and revokes the object URL', () => {
  const originalURL = globalThis.URL
  const originalDocument = globalThis.document
  const originalWindow = globalThis.window
  const calls = []
  const anchor = {
    href: '', download: '', rel: '', hidden: false,
    click: () => calls.push('click'),
    remove: () => calls.push('remove'),
  }
  globalThis.URL = {
    createObjectURL: (blob) => {
      calls.push(`blob:${blob.type}`)
      return 'blob:test'
    },
    revokeObjectURL: (url) => calls.push(`revoke:${url}`),
  }
  globalThis.document = {
    createElement: () => anchor,
    body: { append: () => calls.push('append') },
  }
  globalThis.window = { setTimeout: (callback) => { callback(); return 1 } }
  try {
    downloadMallWeatherBytes(new Uint8Array([1]), 'SH-001_realtime.csv')
    assert.deepEqual(calls, [
      'blob:text/csv;charset=utf-8', 'append', 'click', 'remove', 'revoke:blob:test',
    ])
    assert.equal(anchor.download, 'SH-001_realtime.csv')
    assert.throws(
      () => downloadMallWeatherBytes(new Uint8Array([1]), '../escape.csv'),
      /file name/,
    )
  } finally {
    globalThis.URL = originalURL
    globalThis.document = originalDocument
    globalThis.window = originalWindow
  }
})

test('rejects invalid contexts, rows and non-finite numeric data', () => {
  assert.throws(() => createMallWeatherDatasetCsv('realtime', [], { mallCode: '', mallName: '商场' }), /mall context/)
  assert.throws(() => createMallWeatherDatasetCsv('realtime', [], { mallCode: 'M1', mallName: '' }), /mall context/)
  assert.throws(() => createMallWeatherDatasetCsv('realtime', [{
    snapshotAtLocal: '2026-07-28',
    providerServerTimeLocal: '2026-07-28',
    fetchedAtLocal: '2026-07-28',
    temperatureC: Number.NaN,
    qualityStatus: 'GOOD',
    qualityWarnings: [],
  }], mall), /number/)
  assert.throws(() => createMallWeatherCsvZip(null, mall), /ZIP data/)
})

test('exports chart times in Shanghai even when source points use UTC', () => {
  const csv = decodeCsv(createMallWeatherChartCsv([{ id: 'traffic', name: '客流', unit: '人次', data: [{ time: '2026-10-29T16:03:04Z', value: 10 }] }], mall))
  assert.ok(csv.includes('2026-10-30 00:03:04'))
  assert.ok(!csv.includes('2026-10-29T16:03:04Z'))
  const dataset = decodeCsv(createMallWeatherDatasetCsv('minutely', [{ forecastMinuteUtc: '2026-10-29T16:03:04Z', forecastMinuteLocal: '2026-10-30T00:03:04+08:00' }], mall))
  assert.ok(dataset.includes('2026-10-30 00:03:04,2026-10-30 00:03:04'))
  assert.ok(dataset.includes('上海时间'))
})
