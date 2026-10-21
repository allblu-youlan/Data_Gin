import { useEffect, useState } from 'react'
import { CheckCheck, Columns3, FileText, ListFilter, ShieldCheck, Workflow } from 'lucide-react'
import styles from './ReportConfigDrawer.module.css'

export type ReportEditorTab = 'basic' | 'procedure' | 'conditions' | 'excel' | 'validation' | 'publication'
const steps = {
  basic: { icon: FileText, detail: '名称、分类与连接' },
  procedure: { icon: Workflow, detail: '业务表与关联' },
  conditions: { icon: ListFilter, detail: '参数与筛选规则' },
  excel: { icon: Columns3, detail: '预览列与 Excel' },
  validation: { icon: ShieldCheck, detail: '检查并试运行' },
  publication: { icon: CheckCheck, detail: '查看本次发布结果' },
}

export function ReportEditorNavigation({ tabs, active, datasource, onSelect }: { tabs: Array<{ key: ReportEditorTab; label: string }>; active: ReportEditorTab; datasource: string; onSelect: (tab: ReportEditorTab) => void }) {
  const [horizontal, setHorizontal] = useState(() => window.matchMedia('(max-width: 1000px)').matches)
  useEffect(() => {
    const query = window.matchMedia('(max-width: 1000px)')
    const update = () => setHorizontal(query.matches)
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])
  return <aside className={styles.flowRail}>
    <span className={styles.railCaption}>配置流程</span>
    <div className={styles.tabs} role="tablist" aria-label="报表配置步骤" aria-orientation={horizontal ? 'horizontal' : 'vertical'} onKeyDown={(event) => {
      const current = tabs.findIndex((item) => item.key === active)
      const nextKey = horizontal ? 'ArrowRight' : 'ArrowDown'
      const previousKey = horizontal ? 'ArrowLeft' : 'ArrowUp'
      const index = event.key === nextKey ? (current + 1) % tabs.length : event.key === previousKey ? (current - 1 + tabs.length) % tabs.length : event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : -1
      if (index < 0) return
      event.preventDefault()
      onSelect(tabs[index].key)
      document.getElementById(`report-config-tab-${tabs[index].key}`)?.focus()
    }}>{tabs.map((item, index) => { const Icon = steps[item.key].icon; return <button id={`report-config-tab-${item.key}`} type="button" role="tab" aria-selected={active === item.key} aria-controls="report-config-panel" tabIndex={active === item.key ? 0 : -1} className={active === item.key ? styles.active : ''} onClick={() => onSelect(item.key)} key={item.key}><span className={styles.stepIcon}><Icon aria-hidden="true" /></span><span className={styles.stepCopy}><strong>{item.label}</strong><small>{item.key === 'procedure' && item.label === '存储过程' ? '过程与结果表' : steps[item.key].detail}</small></span><span className={styles.stepNumber} aria-hidden="true">{String(index + 1).padStart(2, '0')}</span></button> })}</div>
    <div className={styles.railConnection}><span className={styles.connectionDot} aria-hidden="true" /><div><strong>Oracle 连接</strong><span>{datasource}</span></div></div>
  </aside>
}
