import { Drawer } from '../ui'
import type { OfficeFeishuBot, OfficeMessage } from './types'
import { messagesForPushChannel, preferredMessageId, type PushTargetDraft } from './pushTargetDraft'
import styles from './OfficeMessage.module.css'

type Props = { draft: PushTargetDraft | null; messages: OfficeMessage[]; bots: OfficeFeishuBot[]; saving: boolean; error: string; onChange: (draft: PushTargetDraft) => void; onClose: () => void; onSave: () => void }

export function PushTargetDrawer({ draft, messages, bots, saving, error, onChange, onClose, onSave }: Props) {
  const availableMessages = draft ? messagesForPushChannel(messages, draft.channel) : messages
  return <Drawer open={Boolean(draft)} title={draft?.id ? '编辑推送配置' : '新增推送配置'} description="选择飞书消息或 WebDAV Excel 文件推送，并绑定要发送的消息。" closeDisabled={saving} onClose={onClose} footer={<><button type="button" disabled={saving} onClick={onClose}>取消</button><button type="button" className={styles.primary} disabled={saving || (draft?.channel === 'FEISHU' && bots.length === 0)} onClick={onSave}>{saving ? '保存中…' : '保存配置'}</button></>}>
    {draft ? <form className={styles.form} onSubmit={(event) => { event.preventDefault(); onSave() }}>
      <label>配置名称<input name="name" autoComplete="off" required maxLength={128} value={draft.name} disabled={saving} onChange={(event) => onChange({ ...draft, name: event.currentTarget.value })} /></label>
      <label>推送通道<select name="channel" value={draft.channel} disabled={saving} onChange={(event) => {
        const channel = event.currentTarget.value as PushTargetDraft['channel']
        const nextMessages = messagesForPushChannel(messages, channel)
        onChange({
          ...draft,
          channel,
          messageId: nextMessages.some((message) => message.id === draft.messageId) ? draft.messageId : preferredMessageId(nextMessages),
          botAppId: channel === 'FEISHU' ? draft.botAppId || bots[0]?.id || '' : draft.botAppId,
        })
      }}><option value="FEISHU">飞书消息</option><option value="WEBDAV">WebDAV Excel 文件</option></select></label>
      <label>消息<select name="messageId" required value={draft.messageId || ''} disabled={saving} onChange={(event) => onChange({ ...draft, messageId: Number(event.currentTarget.value) })}><option value="" disabled>{availableMessages.length === 0 ? (draft.channel === 'WEBDAV' ? '暂无可用的 Excel 消息' : '暂无可用消息') : '选择消息'}</option>{availableMessages.map((message) => <option key={message.id} value={message.id}>{message.name}{message.enabled ? '' : '（已停用）'}</option>)}</select>{draft.channel === 'WEBDAV' ? <small>WebDAV 只能选择 Oracle 导出的 Excel 消息。</small> : null}</label>
      {draft.channel === 'FEISHU' ? <>
        <label>飞书机器人<select name="botAppId" required value={draft.botAppId} disabled={saving || bots.length === 0} onChange={(event) => onChange({ ...draft, botAppId: event.currentTarget.value })}><option value="" disabled>{bots.length === 0 ? '服务端未配置机器人' : '选择机器人'}</option>{bots.map((bot) => <option key={bot.id} value={bot.id}>{bot.name}（{bot.id}）</option>)}</select><small>机器人 App ID：<code>{draft.botAppId || '未配置'}</code>；App Secret 不会返回浏览器。</small></label>
        <label>接收 ID 类型<select name="receiveIdType" value={draft.receiveIdType} disabled={saving} onChange={(event) => onChange({ ...draft, receiveIdType: event.currentTarget.value as PushTargetDraft['receiveIdType'] })}><option value="chat_id">群聊 ID（chat_id）</option><option value="open_id">用户 open_id</option><option value="user_id">企业 user_id</option><option value="union_id">用户 union_id</option><option value="email">邮箱</option></select></label>
        <label>接收 ID<input name="receiveId" autoComplete="off" required maxLength={255} className={styles.mono} value={draft.receiveId} disabled={saving} placeholder={draft.receiveIdType === 'chat_id' ? 'oc_xxxxxxxxx' : ''} onChange={(event) => onChange({ ...draft, receiveId: event.currentTarget.value })} /></label>
      </> : <>
        <label>WebDAV URL<input name="webdavUrl" type="url" inputMode="url" autoComplete="url" required maxLength={2048} className={styles.mono} value={draft.webdavUrl} disabled={saving} placeholder="https://dav.jianguoyun.com/dav" onChange={(event) => onChange({ ...draft, webdavUrl: event.currentTarget.value })} /><small>填写 HTTPS WebDAV 服务地址。</small></label>
        <label>WebDAV 账号<input name="webdavUsername" autoComplete="username" required maxLength={255} value={draft.webdavUsername} disabled={saving} placeholder="name@example.com" onChange={(event) => onChange({ ...draft, webdavUsername: event.currentTarget.value })} /></label>
        <label>WebDAV 应用密码<input name="webdavPassword" type="password" autoComplete="new-password" required={!draft.id || !draft.hasWebdavPassword} maxLength={512} value={draft.webdavPassword} disabled={saving} placeholder={draft.hasWebdavPassword ? '留空保持现有密码' : '请输入应用密码'} onChange={(event) => onChange({ ...draft, webdavPassword: event.currentTarget.value })} /><small>{draft.hasWebdavPassword ? '已配置密码；留空不修改，输入新值则替换。' : '新建 WebDAV 配置时必填，密码不会回显。'}</small></label>
        <label>固定目录<input name="webdavPath" autoComplete="off" required maxLength={1024} className={styles.mono} value={draft.webdavPath} disabled={saving} placeholder="/reports" onChange={(event) => onChange({ ...draft, webdavPath: event.currentTarget.value })} /><small>定时和手动推送均上传到该目录，例如 <code>/reports</code>。</small></label>
      </>}
      <label className={styles.checkbox}><input name="enabled" type="checkbox" checked={draft.enabled} disabled={saving} onChange={(event) => onChange({ ...draft, enabled: event.currentTarget.checked })} /><span>启用推送配置</span></label>
      {error ? <p className={styles.formError} role="alert">{error}</p> : null}
    </form> : null}
  </Drawer>
}
