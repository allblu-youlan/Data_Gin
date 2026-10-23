import { Check, Circle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { FeedbackState } from '../ui'
import { oracleProcedureIdentity, oracleProcedureQualifiedName, oracleProcedureResultLimit, type OracleProcedureRef, type OracleProcedureSummary } from './oracleProcedureConfig'
import styles from './ExcelOperations.module.css'

type Props = { procedures: OracleProcedureSummary[]; current: OracleProcedureRef | null; disabled: boolean; onSelect: (procedure: OracleProcedureRef) => void }

export function OracleProcedureResults({ procedures, current, disabled, onSelect }: Props) {
 const currentKey = current ? oracleProcedureIdentity(current) : ''
 return <div className={styles.stack}>
  <div className={styles.toolbar}>
   <strong>查询结果</strong>
   <p role="status" className={styles.note}>找到 {procedures.length} 个过程 · 本次最多显示 {oracleProcedureResultLimit} 个</p>
  </div>
  {procedures.length ? <>
   <p className={styles.note}>点击一行选择并读取参数。{procedures.length >= oracleProcedureResultLimit ? '已达到显示上限，请细化上方关键词后重新查询。' : '可使用上方过程名或包名关键词缩小结果。'}</p>
   <ul className={styles.procedureResults} aria-label="存储过程查询结果">
    {procedures.map(procedure => {
     const qualifiedName = oracleProcedureQualifiedName(procedure)
     const selected = !procedure.overload && oracleProcedureIdentity(procedure) === currentKey
     return <li key={`${oracleProcedureIdentity(procedure)}.${procedure.overload}`}>
      <Button type="button" variant={selected ? 'secondary' : 'outline'} className={styles.procedureResult} aria-label={`选择存储过程 ${qualifiedName}${procedure.overload ? ` 重载 ${procedure.overload}` : ''}`} aria-pressed={selected} disabled={disabled || Boolean(procedure.overload)} onClick={() => onSelect(procedure)}>
       {selected ? <Check aria-hidden="true" data-icon="inline-start" /> : <Circle aria-hidden="true" data-icon="inline-start" />}
       <span className={styles.procedureResultContent}>
        <span className={styles.procedureName}>{qualifiedName}</span>
        <span className={styles.procedureMeta}>Owner：{procedure.owner} · {procedure.packageName ? `包名：${procedure.packageName}` : '独立过程'} · {procedure.argumentCount} 个参数</span>
        {procedure.overload && <span className={styles.procedureMeta}>重载 {procedure.overload}，暂不支持选择</span>}
       </span>
       {selected && <Badge variant="secondary">已选</Badge>}
      </Button>
     </li>
    })}
   </ul>
  </> : <FeedbackState kind="empty" title="没有匹配的存储过程" description="请核对 Oracle 用户，或缩短过程名、包名关键词后重新查询。" />}
 </div>
}
