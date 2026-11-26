import { Fragment, useId, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { Button } from './components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './components/ui/table'
import { formatApiFieldDescription, formatApiFieldType, type ApiField } from './apiDocs'
import styles from './ApiDocsPage.module.css'

type ApiDocsFieldsProps = {
  id: string
  title: string
  fields: ApiField[]
  request?: boolean
  query?: string
}

type FieldRowsProps = {
  fields: ApiField[]
  groupId: string
  parentPath?: string
  depth: number
  request: boolean
  query: string
  expanded: boolean
  expandMatches: boolean
  hidden?: boolean
}

const initialFieldCount = 8

function matchesField(field: ApiField, query: string): boolean {
  if (!query) return false
  return `${field.name} ${field.type} ${field.description}`.toLocaleLowerCase().includes(query)
    || (field.children ?? []).some(child => matchesField(child, query))
}

function fieldRowIds(fields: ApiField[], groupId: string): string[] {
  const ids = fields.flatMap((field, index) => {
    const rowId = `${groupId}-${index}`
    return [rowId, ...fieldRowIds(field.children ?? [], `${rowId}-children`)]
  })
  if (fields.length > initialFieldCount) ids.push(`${groupId}-more`)
  return ids
}

export default function ApiDocsFields({ id, title, fields, request = false, query = '' }: ApiDocsFieldsProps) {
  const instanceId = useId()
  const term = query.trim().toLocaleLowerCase()
  const [expansion, setExpansion] = useState({ query: term, mode: 'default', revision: 0 })
  const mode = expansion.query === term ? expansion.mode : 'default'
  const expanded = mode === 'expanded'
  const expandable = fields.length > initialFieldCount || fields.some(field => field.children?.length)
  const groupId = `${id}-${instanceId}`

  return <section id={id} className={styles.section} aria-label={title}>
    <div className={styles.sectionHeading}>
      <h2>{title}<span>{fields.length} 个</span></h2>
      {expandable && <Button type="button" variant="ghost" size="sm" aria-expanded={expanded} aria-controls={`${groupId}-body`} onClick={() => setExpansion(previous => ({ query: term, mode: expanded ? 'collapsed' : 'expanded', revision: previous.revision + 1 }))}>{expanded ? '全部收起' : '全部展开'}</Button>}
    </div>
    {fields.length ? <Table className={styles.fieldTable} aria-label={title} role="table">
      <TableHeader role="rowgroup"><TableRow role="row"><TableHead role="columnheader">字段</TableHead><TableHead role="columnheader">类型</TableHead><TableHead role="columnheader">含义</TableHead></TableRow></TableHeader>
      <TableBody id={`${groupId}-body`} role="rowgroup">
        <FieldRows key={`${term}-${expansion.revision}`} fields={fields} groupId={groupId} depth={0} request={request} query={term} expanded={expanded} expandMatches={mode !== 'collapsed'} />
      </TableBody>
    </Table> : <p className={styles.fieldMeaning}>{request ? '无需提供参数。' : '暂无响应字段说明。'}</p>}
  </section>
}

function FieldRows({ fields, groupId, parentPath = '', depth, request, query, expanded, expandMatches, hidden = false }: FieldRowsProps) {
  const [showAll, setShowAll] = useState(expanded)
  const remaining = fields.slice(initialFieldCount).filter(field => !matchesField(field, query)).length
  const extraRowIds = fields.slice(initialFieldCount).flatMap((field, index) => {
    const rowId = `${groupId}-${index + initialFieldCount}`
    return [rowId, ...fieldRowIds(field.children ?? [], `${rowId}-children`)]
  })

  return <>
    {fields.map((field, index) => <FieldRow key={`${index}-${field.name}`} field={field} rowId={`${groupId}-${index}`} parentPath={parentPath} depth={depth} request={request} query={query} expanded={expanded} expandMatches={expandMatches} hidden={hidden || (!showAll && index >= initialFieldCount && !matchesField(field, query))} />)}
    {fields.length > initialFieldCount && <TableRow role="row" id={`${groupId}-more`} hidden={hidden || (!showAll && remaining === 0)}>
      <TableCell role="cell" colSpan={3}><Button type="button" variant="ghost" className={styles.more} aria-expanded={showAll} aria-controls={extraRowIds.join(' ')} onClick={() => setShowAll(value => !value)}>{showAll ? '收起字段' : `展开其余 ${remaining} 个字段`}</Button></TableCell>
    </TableRow>}
  </>
}

function FieldRow({ field, rowId, parentPath, depth, request, query, expanded, expandMatches, hidden }: Omit<FieldRowsProps, 'fields' | 'groupId'> & { field: ApiField; rowId: string }) {
  const children = field.children ?? []
  const [open, setOpen] = useState(() => expanded || (expandMatches && children.some(child => matchesField(child, query))))
  const type = formatApiFieldType(field.type)
  const displayType = field.nullable && !type.split(' | ').includes('null') ? `${type} | null` : type
  const fullPath = `${parentPath ? `${parentPath}.` : ''}${field.name}`
  const childGroupId = `${rowId}-children`
  const location = request && field.location && field.location !== 'body' ? `${field.location} 参数。` : ''

  return <Fragment>
    <TableRow role="row" id={rowId} hidden={hidden}>
      <TableHead role="rowheader" scope="row" className={styles.fieldName} title={fullPath}>
        <div className={styles.fieldNameInner} style={{ paddingInlineStart: Math.min(depth, 3) * 12 }}>
          {children.length ? <Button type="button" variant="ghost" className={styles.fieldToggle} data-expanded={open} aria-expanded={open} aria-controls={fieldRowIds(children, childGroupId).join(' ')} aria-label={`${open ? '收起' : '展开'} ${fullPath} 的子字段`} onClick={() => setOpen(value => !value)}>
            {open ? <ChevronDown aria-hidden="true" data-icon="inline-start" /> : <ChevronRight aria-hidden="true" data-icon="inline-start" />}
            <code>{field.name}</code><span className={styles.expandLabel}>{open ? '收起子字段' : '展开子字段'}</span>
          </Button> : <code>{field.name}</code>}
          {request && field.required && <span className={styles.required}>必填</span>}
        </div>
      </TableHead>
      <TableCell role="cell" className={styles.fieldType}><code>{displayType}</code></TableCell>
      <TableCell role="cell" className={styles.fieldMeaning}>{location}{formatApiFieldDescription(field.description)}</TableCell>
    </TableRow>
    {!!children.length && <FieldRows fields={children} groupId={childGroupId} parentPath={`${fullPath}${type.includes('[]') ? '[]' : ''}`} depth={depth + 1} request={request} query={query} expanded={expanded} expandMatches={expandMatches} hidden={hidden || !open} />}
  </Fragment>
}
