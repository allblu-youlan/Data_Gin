import { useId, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { runSingleFlight } from '../singleFlight'
import { Section } from '../ui'
import { mentionRequest } from './api'
import { mentionDigestSettingsRequest } from './contracts'
import type { DigestSettings } from './types'
import styles from './OfficeMention.module.css'

type Props = {
  client: WorkspaceApiClient
  permissions: string[]
  currentTime: string | null
  loading: boolean
  onSaved: () => void
}

export function DigestSettingsSection({ client, permissions, currentTime, loading, onSaved }: Props) {
  const [draft, setDraft] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState<{ text: string; error: boolean } | null>(null)
  const inFlight = useRef(false)
  const hintId = useId()
  const errorId = useId()
  const canManage = permissions.includes('office_mention.manage')

  async function save(event: FormEvent) {
    event.preventDefault()
    if (!canManage || loading) return
    await runSingleFlight(inFlight, async () => {
      setSaving(true)
      setNotice(null)
      try {
        const request = mentionDigestSettingsRequest(draft ?? currentTime ?? '')
        const result = await mentionRequest<DigestSettings>(client, request.path, request.options)
        setDraft(result.digestTime)
        setNotice({ text: '已保存，无需重启，调度器通常一分钟内应用；模块关闭时启用后按该时间运行。', error: false })
        onSaved()
      } catch (reason) {
        setNotice({ text: reason instanceof Error ? reason.message : '保存失败，请稍后重试。', error: true })
      } finally {
        setSaving(false)
      }
    })
  }

  return <Section title="日报发送时间" description="按上海时间每天投递。此设置只修改时间，不会开启模块或日报开关。">
    <div className={styles.detail}>
      <dl className={styles.facts}>
        <dt>当前生效时刻</dt><dd>{loading ? '正在读取…' : currentTime ?? '未知'}</dd>
        <dt>时区</dt><dd>Asia/Shanghai（上海时间）</dd>
      </dl>
      {!loading && currentTime === null ? <p className={styles.notice}>尚未获取当前设置。可刷新状态，或填写新的时间后保存。</p> : null}
      {canManage ? <form className={styles.form} onSubmit={(event) => void save(event)} noValidate aria-busy={saving}>
        <label>每日发送时间（上海）
          <input
            type="time"
            step={60}
            required
            value={draft ?? currentTime ?? ''}
            disabled={saving}
            aria-describedby={`${hintId}${notice?.error ? ` ${errorId}` : ''}`}
            onChange={(event) => { setDraft(event.target.value); setNotice(null) }}
          />
        </label>
        <p id={hintId} className={styles.notice}>保存后通常一分钟内应用，无需重启。已生成日报内容不变，已到期积压仍按原统计窗口补建。</p>
        <div className={styles.actions}><button type="submit" className={styles.primary} disabled={saving || loading}>{saving ? '保存中…' : '保存日报时间'}</button></div>
      </form> : null}
      {notice ? <p id={notice.error ? errorId : undefined} role={notice.error ? 'alert' : 'status'} className={notice.error ? styles.error : styles.success}>{notice.text}</p> : null}
    </div>
  </Section>
}
