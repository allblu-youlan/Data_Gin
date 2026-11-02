import { formatShanghaiDateTime } from '../../../dateTime.js'
import { CheckCircle2 } from 'lucide-react'
import { StatusTag } from '../../../ui'
import type { ReportPublication } from '../../types'
import styles from './ReportValidationResultDrawer.module.css'

export function ReportValidationResultContent({ publication }: { publication: ReportPublication }) {
  const result = publication.validation
  return (
      result?.executionMode === 'SELECT_SNAPSHOT' ? <div className={styles.content}><div className={styles.summary} role="status"><CheckCircle2 aria-hidden="true" /><div><strong>自定义查询契约核验通过</strong><span>{formatDate(result.validatedAt)}</span></div><StatusTag tone="success">{publication.status || 'PUBLISHED'}</StatusTag></div><ResultSection title="查询定义" rows={[["表 1 目录 ID", result.query.baseTableId], ["独立关联", result.query.relationCount], ["输出列", result.query.columnCount], ["查询 Schema Hash", result.query.schemaHash]]} /><ResultSection title="结果与导出" rows={[["导出方式", result.executionPolicy.exportPolicy === 'MANUAL' ? '手动导出' : '自动导出'], ["结果保留", `${result.executionPolicy.resultRetentionHours} 小时`], ["导出后清理", result.executionPolicy.purgeAfterExport ? '是' : '保留至到期'], ["允许导出字段", result.export.exportableColumnCount], ["完整契约 Hash", publication.contractHash]]} /></div> : result ? <div className={styles.content}>
        <div className={styles.summary} role="status"><CheckCircle2 aria-hidden="true" /><div><strong>Oracle 契约核验通过</strong><span>{formatDate(result.validatedAt)}</span></div><StatusTag tone="success">{publication.status || 'PUBLISHED'}</StatusTag></div>
        <ResultSection title="存储过程" rows={[
          ['过程', qualifiedName(result.procedure.owner, result.procedure.package, result.procedure.name)],
          ['Overload', result.procedure.overload || '—'],
          ['过程参数数量', result.procedure.argumentCount],
          ['签名 Hash', result.procedure.signatureHash],
        ]} />
        <ResultSection title="结果表" rows={[
          ['结果表', qualifiedName(result.result.tableOwner, '', result.result.tableName)],
          ['字段数量', result.result.columnCount],
          ['结果 Schema Hash', result.result.schemaHash],
          ['结果表核验', result.snapshot.resultTableValidated ? '通过' : '未通过'],
          ['取数方式', '整表读取'],
          ['清理策略', 'Excel 导出成功后清理'],
        ]} />
        <ResultSection title="Excel 与发布契约" rows={[
          ['允许导出字段', result.export.exportableColumnCount],
          ['导出 Schema Hash', result.export.schemaHash],
          ['完整契约 Hash', publication.contractHash],
          ['发布时间', publication.publishedAt ? formatDate(publication.publishedAt) : '—'],
        ]} />
      </div> : <div className={styles.compatibility} role="status"><strong>报表已成功发布</strong><p>当前服务实例未返回校验摘要。请在服务滚动升级完成后发布新版本，即可查看过程签名、结果 Schema 与 Excel 契约核验详情。</p></div>
  )
}

function ResultSection({ title, rows }: { title: string; rows: Array<[string, unknown]> }) {
  return <section className={styles.section}><h3>{title}</h3><dl>{rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{String(value ?? '—')}</dd></div>)}</dl></section>
}

function qualifiedName(owner: string, packageName: string, name: string) {
  return [owner, packageName, name].filter(Boolean).join('.') || '—'
}

function formatDate(value: string) { return formatShanghaiDateTime(value, '—') }
