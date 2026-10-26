export type OracleImportStage = 'counting' | 'importing' | 'committing' | 'completed' | 'failed'
export type OracleImportProgress = {
  stepIndex: number
  stepName: string
  stage: OracleImportStage
  processedRows: number
  totalRows: number
  totalKnown: boolean
  scannedRows: number
  fileIndex: number
  fileCount: number
  fileName: string
  sheetName: string
  rowNumber: number
  identifier: string
  elapsedMillis?: number
  countMillis?: number
  readMillis?: number
  writeMillis?: number
  batchRows?: number
  batches?: number
}

const stages: OracleImportStage[] = ['counting', 'importing', 'committing', 'completed', 'failed']
function record(value: unknown): Record<string, unknown> | null { return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null }
function count(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 }

export function parseOracleImportProgress(value: unknown): OracleImportProgress | null {
  const item = record(value)
  if (!item || !stages.includes(item.stage as OracleImportStage) || !count(item.stepIndex) || item.stepIndex < 1 || typeof item.totalKnown !== 'boolean') return null
  if (!['processedRows', 'totalRows', 'scannedRows', 'fileIndex', 'fileCount', 'rowNumber'].every((key) => count(item[key]))) return null
  if (!['stepName', 'fileName', 'sheetName', 'identifier'].every((key) => typeof item[key] === 'string')) return null
  const metrics: Partial<OracleImportProgress> = {}
  for (const key of ['elapsedMillis', 'countMillis', 'readMillis', 'writeMillis', 'batchRows', 'batches'] as const) {
    const value = item[key]
    if (count(value)) metrics[key] = value
  }
  return {
    stepIndex: item.stepIndex, stepName: item.stepName as string, stage: item.stage as OracleImportStage,
    processedRows: item.processedRows as number, totalRows: item.totalRows as number, totalKnown: item.totalKnown,
    scannedRows: item.scannedRows as number, fileIndex: item.fileIndex as number, fileCount: item.fileCount as number,
    fileName: item.fileName as string, sheetName: item.sheetName as string, rowNumber: item.rowNumber as number, identifier: item.identifier as string,
    ...metrics,
  }
}

function durationLabel(milliseconds: number): string {
  if (milliseconds < 60000) return `${(milliseconds / 1000).toLocaleString('zh-CN', { maximumFractionDigits: 1 })} 秒`
  const seconds = Math.floor(milliseconds / 1000)
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor(seconds % 3600 / 60)
  return `${hours ? `${hours} 小时 ` : ''}${minutes} 分 ${seconds % 60} 秒`
}

export function oracleImportTimingView(progress: OracleImportProgress): string[] {
  const lines: string[] = []
  if (progress.elapsedMillis !== undefined && progress.elapsedMillis > 0) {
    const speed = progress.processedRows * 1000 / progress.elapsedMillis
    lines.push(`累计耗时 ${durationLabel(progress.elapsedMillis)}${progress.processedRows > 0 ? ` · 平均写入速度 ${speed.toLocaleString('zh-CN', { maximumFractionDigits: 1 })} 行/秒` : ''}`)
  }
  const phases = [['统计', progress.countMillis], ['读取', progress.readMillis], ['Oracle 写入', progress.writeMillis]] as const
  if (phases.some(([, value]) => value !== undefined && value > 0)) {
    lines.push(`${phases.flatMap(([name, value]) => value === undefined ? [] : [`${name} ${durationLabel(value)}`]).join(' · ')}（耗时可能重叠，不相加）`)
  }
  const batch: string[] = []
  if (progress.batchRows !== undefined && progress.batchRows > 0) batch.push(`批次行数 ${progress.batchRows.toLocaleString('en-US')}`)
  if (progress.batches !== undefined && progress.batches > 0) batch.push(`已执行 ${progress.batches.toLocaleString('en-US')} 批`)
  if (batch.length) lines.push(batch.join(' · '))
  return lines
}

export function latestOracleImportProgress(logs: readonly unknown[], jobID: number): OracleImportProgress | null {
  let latest: Record<string, unknown> | null = null
  for (const value of logs) {
    const log = record(value)
    if (!log || log.job_id !== jobID || !count(log.id) || log.id < 1 || log.import_progress === undefined) continue
    if (!latest || log.id > Number(latest.id)) latest = log
  }
  return latest ? parseOracleImportProgress(latest.import_progress) : null
}

export function oracleImportProgressView(progress: OracleImportProgress, jobStatus: string) {
  const stage = jobStatus === 'failed' && progress.stage !== 'completed' ? 'failed' : progress.stage
  const rows = progress.processedRows.toLocaleString('en-US')
  const total = progress.totalKnown ? progress.totalRows.toLocaleString('en-US') : '总数未知'
  const written = progress.totalKnown ? `${rows} / ${total}` : rows
  const percent = stage === 'counting' ? undefined : stage === 'completed' ? 100 : !progress.totalKnown ? undefined : stage === 'failed' || progress.totalRows === 0 ? 0 : Math.min(100, Math.round(progress.processedRows / progress.totalRows * 100))
  if (stage === 'counting') return { stage, label: '正在统计', percent, headline: `正在统计导入行数，已扫描 ${progress.scannedRows.toLocaleString('en-US')} 行`, note: '统计完成后显示总行数，当前尚未导入。' }
  if (stage === 'failed') return { stage, label: '已回滚', percent, headline: `已回滚，本次成功导入 0 行${progress.totalKnown ? ` / ${total} 行` : ''}`, note: `回滚前已写入 ${rows} 行，这些行未成功提交。此前其他步骤已提交的数据仍保留。` }
  if (stage === 'completed') return { stage, label: '导入成功', percent, headline: `成功导入 ${written} 行（100%）`, note: '导入事务已提交。' }
  if (stage === 'committing') return { stage, label: '正在提交', percent, headline: `正在提交事务，已写入 ${written} 行${percent === undefined ? '' : `（${percent}%）`}`, note: '尚未确认成功，等待事务提交完成。' }
  return { stage, label: '正在写入', percent, headline: `已写入 ${written} 行${percent === undefined ? '' : `（${percent}%）`}`, note: '待事务提交，当前写入行数尚未成为成功导入行数。' }
}
