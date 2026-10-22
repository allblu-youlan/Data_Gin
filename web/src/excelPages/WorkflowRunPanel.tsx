import { Download, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Section } from '../ui'
import { excelJobStatusLabel } from './excelPageSupport'
import type { useExcelJobs } from './useExcelJobs'
import styles from './ExcelOperations.module.css'

export function WorkflowRunPanel({ jobs, onNavigateToJobs }: { jobs: ReturnType<typeof useExcelJobs>; onNavigateToJobs: () => void }) {
 const { job, jobLogs } = jobs
 if (!job) return null
 return <Section title={`执行记录 #${job.id}`} actions={<div className={styles.actions}><Badge variant={job.status === 'failed' ? 'destructive' : 'secondary'}>{excelJobStatusLabel(job.status)}</Badge><Button type="button" variant="outline" size="sm" onClick={() => void jobs.refreshJobByID(job.id)}><RefreshCw aria-hidden="true" />刷新</Button><Button type="button" variant="ghost" size="sm" onClick={onNavigateToJobs}>全部任务</Button></div>}>
  <div className={styles.stack}>
   <div className={styles.runPanel} aria-live="polite"><div className={styles.toolbar}><strong>已完成 {job.processed_rows} / {job.total_rows || '—'} 步</strong><span className={styles.note}>{jobs.autoRefreshText}</span></div><progress aria-label="组合操作执行进度" max={job.total_rows || 1} value={job.processed_rows} /></div>
   {job.status === 'failed' && <p role="alert" className={styles.note}>任务已停止。请查看最后一条步骤日志；此前成功提交的数据仍然保留。</p>}
   <ol className={styles.logs} aria-label="执行日志">{jobLogs.map(log => <li key={log.id} className={`${styles.log} ${log.level === 'error' ? styles.logError : ''}`}><time dateTime={new Date(log.created_at * 1000).toISOString()}>{new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(log.created_at * 1000))}</time><span>{log.message}</span></li>)}</ol>
   {job.can_download && <div><Button type="button" variant="outline" disabled={jobs.downloadingJobID === job.id} onClick={() => void jobs.downloadJob(job.id)}><Download aria-hidden="true" />下载结果</Button></div>}
  </div>
 </Section>
}
