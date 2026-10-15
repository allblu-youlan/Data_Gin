import { useId } from 'react'
import { ArrowDown, ArrowRight, ArrowUp, ChevronDown, Trash2 } from 'lucide-react'
import { isExcelMatchStepComplete, type ExcelMatchFilterConfig, type ExcelMatchModel, type ExcelMatchStepConfig } from '../excelMatchConfig'
import { ExcelMatchFilterRow } from './ExcelMatchFilterRow'
import { ExcelMatchStepFields, Field } from './ExcelMatchPageParts'
import styles from './ExcelMatchPage.module.css'

type Props = {
  step: ExcelMatchStepConfig
  index: number
  count: number
  expanded: boolean
  models: ExcelMatchModel[]
  onExpand: () => void
  onToggle: () => void
  onMove: (direction: -1 | 1) => void
  onRemove: () => void
  onChange: (key: Exclude<keyof ExcelMatchStepConfig, 'filters' | 'enabled'>, value: string) => void
  onModelChange: (value: string) => void
  onFilterChange: (index: number, patch: Partial<ExcelMatchFilterConfig>) => void
  onFilterAdd: () => void
  onFilterRemove: (index: number) => void
}

const modeLabels = { field: '字段匹配', order_item_sku: '商品 SKU', conditional_write: '条件写入' }

export function ExcelRuleEditor({ step, index, count, expanded, models, onExpand, onToggle, onMove, onRemove, onChange, onModelChange, onFilterChange, onFilterAdd, onFilterRemove }: Props) {
  const detailID = useId()
  const enabled = step.enabled !== false
  const complete = isExcelMatchStepComplete(step)
  return (
    <article className={`${styles.rule} ${!enabled ? styles.ruleDisabled : ''} ${expanded ? styles.ruleExpanded : ''}`}>
      <div className={styles.ruleHeader}>
        <button type="button" className={styles.ruleSummary} onClick={onExpand} aria-expanded={expanded} aria-controls={detailID}>
          <span className={styles.ruleNumber}>{String(index + 1).padStart(2, '0')}</span>
          <span className={styles.ruleTitle}>
            <strong>{step.name || `规则 ${index + 1}`}</strong>
            <span>{modeLabels[step.matchMode]}<span className={styles.ruleDivider}>·</span>{step.filters.length ? `${step.filters.length} 条筛选` : '全部行'}</span>
          </span>
          <span className={styles.ruleFlow}><span title={step.matchExcelColumn}>{step.matchExcelColumn || '输入待配置'}</span><ArrowRight aria-hidden="true" /><span title={step.outputColumnName}>{step.outputColumnName || '输出待配置'}</span></span>
          <span className={!enabled ? styles.ruleStatus : complete ? styles.stepComplete : styles.stepIncomplete}>{!enabled ? '不参与执行' : complete ? '配置完整' : '待完善'}</span>
          <ChevronDown className={styles.ruleChevron} aria-hidden="true" />
        </button>
        <button type="button" role="switch" aria-checked={enabled} aria-label={`启用规则 ${index + 1}：${step.name}`} className={styles.ruleToggle} onClick={onToggle}>
          <span className={styles.toggleTrack} aria-hidden="true"><span /></span>{enabled ? '已启用' : '已停用'}
        </button>
      </div>
      <div id={detailID} hidden={!expanded} className={styles.ruleDetail}>
        <div className={styles.ruleDetailHeading}>
          <span>{enabled ? '修改后保存方案，或直接预览本次配置。' : '规则已停用，配置会保留；重新启用后可编辑并参与执行。'}</span>
          <div className={styles.tableActions}>
            <button type="button" onClick={() => onMove(-1)} disabled={index === 0} aria-label={`上移规则 ${index + 1}`}><ArrowUp aria-hidden="true" />上移</button>
            <button type="button" onClick={() => onMove(1)} disabled={index === count - 1} aria-label={`下移规则 ${index + 1}`}><ArrowDown aria-hidden="true" />下移</button>
            <button type="button" className={styles.danger} onClick={onRemove} disabled={count === 1} aria-label={`删除规则 ${index + 1}`}><Trash2 aria-hidden="true" />删除</button>
          </div>
        </div>
        <fieldset className={styles.ruleFields} disabled={!enabled} data-step-index={index}>
          <legend className={styles.visuallyHidden}>规则 {index + 1} 配置</legend>
          <ExcelMatchStepFields step={step} index={index} models={models} onChange={onChange} onModelChange={onModelChange}>
            <Field label="规则名称" name={`step_name_${index}`} value={step.name} onChange={(value) => onChange('name', value)} required />
          </ExcelMatchStepFields>
          {step.matchMode === 'order_item_sku' && <p className={styles.modeNote}>优先用规格编码匹配 mProductName 前缀，同时校验销售金额和数量，输出完整 no；规格编码为 15 位或 16 位时跳过。</p>}
          <div className={styles.stepFilterEditor}>
            <div className={styles.stepFilterHeading}><div><strong>本规则筛选</strong><span>{step.matchMode === 'order_item_sku' ? '不同条件需同时满足；多值条件内匹配任意一项即可。仅可引用未被前序条件写入修改的原始列。' : '不同条件需同时满足；多值条件内匹配任意一项即可。可引用原始列或前序已启用规则的结果。'}</span></div><button type="button" onClick={onFilterAdd}>添加条件</button></div>
            {step.filters.length === 0 && <p className={styles.stepFilterEmpty}>未设置筛选，将处理全部 Excel 行。</p>}
            {step.filters.map((filter, filterIndex) => (
              <ExcelMatchFilterRow key={filterIndex} filter={filter} stepIndex={index} index={filterIndex} onChange={(patch) => onFilterChange(filterIndex, patch)} onRemove={() => onFilterRemove(filterIndex)} />
            ))}
          </div>
        </fieldset>
      </div>
    </article>
  )
}
