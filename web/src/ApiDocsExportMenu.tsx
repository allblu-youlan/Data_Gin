import { useState } from 'react'
import { DropdownMenu } from 'radix-ui'
import { Check, Download } from 'lucide-react'
import { Button } from './components/ui/button'
import { apiMarkdownFilename, buildApiMarkdown, type ApiDocument } from './apiDocs'
import styles from './ApiDocsExportMenu.module.css'

type Props = {
  document: ApiDocument | null
  currentOperationId?: string
  baseURL: string
  disabled?: boolean
}

export default function ApiDocsExportMenu({ document, currentOperationId, baseURL, disabled }: Props) {
  const [feedback, setFeedback] = useState('')
  const [failed, setFailed] = useState(false)
  const [format, setFormat] = useState('md')
  const [busy, setBusy] = useState(false)
  const hasOperations = document?.groups.some(group => group.children.some(child => child.operations.length > 0))

  async function download(operationId?: string) {
    if (!document) return
    setBusy(true)
    setFailed(false)
    setFeedback(format === 'docx' ? '正在生成 Word 文档…' : '正在生成 Markdown 文档…')
    try {
      const blob = format === 'docx'
        ? await (await import('./apiDocsWord')).buildApiWord(document, baseURL, operationId)
        : new Blob([buildApiMarkdown(document, baseURL, operationId)], { type: 'text/markdown;charset=utf-8' })
      const filename = apiMarkdownFilename(document, operationId).replace(/\.md$/, `.${format}`)
      const url = URL.createObjectURL(blob)
      const anchor = window.document.createElement('a')
      anchor.href = url
      anchor.download = filename
      anchor.hidden = true
      try {
        window.document.body.append(anchor)
        anchor.click()
      } finally {
        anchor.remove()
        window.setTimeout(() => URL.revokeObjectURL(url), 1000)
      }
      setFailed(false)
      setFeedback(`已开始下载 ${format === 'docx' ? 'Word' : 'Markdown'} 文件`)
    } catch {
      setFailed(true)
      setFeedback('导出失败，请重新选择导出范围重试。')
    } finally {
      setBusy(false)
    }
  }

  return <div className={styles.root}>
    <DropdownMenu.Root onOpenChange={open => { if (open) setFeedback('') }}>
      <DropdownMenu.Trigger asChild>
        <Button variant="outline" aria-label="导出文档" title="导出文档" disabled={disabled || !hasOperations || busy}>
          <Download aria-hidden="true" data-icon="inline-start" /><span className={styles.label}>{busy ? '正在导出…' : '导出文档'}</span>
        </Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className={styles.menu} align="end" sideOffset={8} collisionPadding={12}>
          <DropdownMenu.RadioGroup aria-label="导出格式" value={format} onValueChange={setFormat}>
            {[['md', 'Markdown (.md)'], ['docx', 'Word (.docx)']].map(([value, label]) => <DropdownMenu.RadioItem key={value} className={styles.item} value={value} onSelect={event => event.preventDefault()}><span className={styles.indicator}><DropdownMenu.ItemIndicator><Check size={14} aria-hidden="true" /></DropdownMenu.ItemIndicator></span>{label}</DropdownMenu.RadioItem>)}
          </DropdownMenu.RadioGroup>
          <DropdownMenu.Separator className={styles.separator} />
          <DropdownMenu.Group>
            <DropdownMenu.Item className={styles.item} disabled={!currentOperationId} onSelect={() => { if (currentOperationId) void download(currentOperationId) }}>导出当前接口</DropdownMenu.Item>
            <DropdownMenu.Item className={styles.item} onSelect={() => void download()}>导出全部文档</DropdownMenu.Item>
          </DropdownMenu.Group>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
    <span className={styles.feedback} role={failed ? 'alert' : 'status'}>{feedback}</span>
  </div>
}
