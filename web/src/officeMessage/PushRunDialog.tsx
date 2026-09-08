import { Dialog } from '../ui'
import type { OfficeMessage, OfficePushTarget } from './types'
import styles from './OfficeMessage.module.css'

type Props = { target: OfficePushTarget | null; message: OfficeMessage | null; values: Record<string, string>; sending: boolean; error: string; onValuesChange: (values: Record<string, string>) => void; onClose: () => void; onConfirm: () => void }

export function PushRunDialog({ target, message, values, sending, error, onValuesChange, onClose, onConfirm }: Props) {
  const webDAV = target?.channel === 'WEBDAV'
  const description = webDAV
    ? '任务会持久化入队，Excel 在后台生成后上传到固定目录。'
    : message?.sourceType === 'EDITED' ? '任务会持久化入队，消息将在后台发送。' : '任务会持久化入队，Excel 在后台生成后发送。'
  return <Dialog open={Boolean(target)} title={webDAV ? '确认上传 WebDAV' : '确认发送飞书消息'} description={description} closeDisabled={sending} closeOnBackdrop={!sending} onClose={onClose} footer={<><button type="button" disabled={sending} onClick={onClose}>取消</button><button type="button" className={styles.primary} disabled={sending || !message} onClick={onConfirm}>{sending ? '提交中…' : '确认推送'}</button></>}>
    {target ? <div className={styles.runForm}><p>推送配置：<strong>{target.name}</strong></p><p>通道：<strong>{webDAV ? 'WebDAV Excel 文件' : '飞书消息'}</strong></p>{webDAV ? <><p>WebDAV 账号：<code>{target.webdavUsername}</code></p><p>上传位置：<code>{target.webdavUrl}{target.webdavPath}</code></p></> : <><p>机器人 App ID：<code>{target.botAppId}</code></p><p>接收方：<code>{target.receiveIdType}:{target.receiveId}</code></p></>}{message?.parameters.map((parameter) => <label key={parameter.code}>{parameter.label}{parameter.required ? ' *' : ''}<input required={parameter.required} className={parameter.valueType === 'string' ? undefined : styles.mono} value={values[parameter.code] ?? ''} placeholder={parameter.valueType === 'date' ? parameter.format : parameter.valueType} disabled={sending} onChange={(event) => onValuesChange({ ...values, [parameter.code]: event.currentTarget.value })} /><small>参数名 <code>:{parameter.code}</code>{parameter.format ? `，格式 ${parameter.format}` : ''}</small></label>)}{error ? <p className={styles.formError} role="alert">{error}</p> : null}</div> : null}
  </Dialog>
}
