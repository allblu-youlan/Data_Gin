import { useState } from 'react'
import { Section, StatusTag } from '../ui'
import type { DetailAccess } from './types'
import styles from './OfficeMention.module.css'

export function DetailAccessSection({ access }: { access: DetailAccess | undefined }) {
  const [notice, setNotice] = useState<{ text: string; error: boolean } | null>(null)

  async function copyCallback() {
    if (!access?.callbackUrl) return
    try {
      if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(access.callbackUrl)
      setNotice({ text: '已复制完整回调地址。', error: false })
    } catch {
      setNotice({ text: '复制失败，请手动选择并复制上方完整地址。', error: true })
    }
  }

  return <Section title="详情访问配置" description="飞书侧栏详情使用当前应用的 OAuth 登录重定向地址。">
    <div className={styles.detail}>
      <dl className={styles.facts}>
        <dt>当前应用 App ID</dt><dd><code>{access?.appId || '未获取'}</code></dd>
        <dt>公开访问地址</dt><dd><code>{access?.publicUrl || '未配置'}</code></dd>
        <dt>地址校验</dt><dd><StatusTag tone={access?.valid ? 'success' : 'neutral'}>{access ? access.valid ? '本地地址有效' : '待修正配置' : '尚未获取配置'}</StatusTag></dd>
        <dt>完整回调地址</dt><dd>{access?.callbackUrl ? <code className={styles.callbackURL}>{access.callbackUrl}</code> : '未生成'}</dd>
      </dl>
      <div className={styles.actions}><button type="button" disabled={!access?.callbackUrl} onClick={() => void copyCallback()}>复制完整回调地址</button></div>
      {notice ? <p role={notice.error ? 'alert' : 'status'} className={notice.error ? styles.error : styles.success}>{notice.text}</p> : null}
      {access?.hint ? <p className={styles.notice}>{access.hint}</p> : null}
      <p className={styles.notice}>这里的完整回调地址仅用于 OAuth 登录。请在飞书开放平台中打开上方 App ID 对应的同一个应用，进入“安全设置 → 重定向 URL”，配置此地址。本地地址校验通过不代表飞书后台已完成配置。</p>
      <p className={styles.notice}>在“事件与回调 → 回调配置”中选择“长连接”，不要把这里的 OAuth 回调地址填入事件请求地址。如果提示“Challenge code 未返回”，请移除误填的事件请求地址并改用长连接。</p>
      <p className={styles.notice}>遇到错误 20029 时，请核对重定向 URL 与这里的地址完全一致，包括协议、域名、端口和路径。</p>
      <p className={styles.notice}>修改 APP_URL 后，需要重启 API 服务和 worker，再刷新本页确认生效地址。</p>
    </div>
  </Section>
}
