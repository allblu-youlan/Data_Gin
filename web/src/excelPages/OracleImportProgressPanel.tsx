import { Badge } from '@/components/ui/badge'
import type { ExcelMatchJob, ExcelMatchJobLog } from './excelPageSupport'
import { latestOracleImportProgress, oracleImportProgressView } from './oracleImportProgress'
import styles from './ExcelOperations.module.css'

export function OracleImportProgressPanel({ job, logs }: { job: ExcelMatchJob; logs: ExcelMatchJobLog[] }) {
  const progress = latestOracleImportProgress(logs, job.id)
  if (!progress) return null
  const view = oracleImportProgressView(progress, job.status)
  return <section className={styles.runPanel} aria-label="Oracle 导入行进度" aria-live="polite">
    <div className={styles.toolbar}><strong>Oracle 导入行进度 · 第 {progress.stepIndex} 步{progress.stepName ? ` ${progress.stepName}` : ''}</strong><Badge variant={view.stage === 'failed' ? 'destructive' : 'secondary'}>{view.label}</Badge></div>
    <strong>{view.headline}</strong>
    <progress aria-label="Oracle 导入行数进度" aria-valuetext={view.headline} max={100} value={view.percent} />
    <p className={styles.note}>{view.note}</p>
    <div className={styles.stack}>
      <p className={styles.note}>当前文件{progress.fileCount > 0 ? `（${progress.fileIndex} / ${progress.fileCount}）` : ''}：{progress.fileName || '待扫描'}</p>
      <p className={styles.note}>当前 Sheet：{progress.sheetName || '待扫描'}{progress.rowNumber > 0 ? ` · Excel 第 ${progress.rowNumber.toLocaleString('en-US')} 行` : ''}</p>
      {progress.identifier && <p className={styles.note}>导入标号：{progress.identifier}</p>}
    </div>
  </section>
}
