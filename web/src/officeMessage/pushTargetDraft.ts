import type { OfficeFeishuBot, OfficeMessage, OfficePushChannel, OfficePushTarget, OfficeReceiveIDType } from './types'

export const DEFAULT_WEBDAV_URL = 'https://dav.jianguoyun.com/dav'

function isValidWebDAVPath(value: string) {
  const path = value.trim()
  const hasControlCharacter = Array.from(path).some((character) => {
    const code = character.charCodeAt(0)
    return code <= 0x1f || (code >= 0x7f && code <= 0x9f)
  })
  return path.startsWith('/')
    && !path.includes('\\')
    && !hasControlCharacter
    && !path.split('/').some((segment) => segment === '.' || segment === '..')
}

export type PushTargetDraft = {
  id: number | null
  name: string
  messageId: number
  channel: OfficePushChannel
  botAppId: string
  receiveIdType: OfficeReceiveIDType
  receiveId: string
  webdavUrl: string
  webdavUsername: string
  webdavPassword: string
  webdavPath: string
  hasWebdavPassword: boolean
  enabled: boolean
  lockVersion: number
}

export function emptyPushTarget(messages: OfficeMessage[], bots: OfficeFeishuBot[]): PushTargetDraft {
  const channel: OfficePushChannel = bots.length === 0 ? 'WEBDAV' : 'FEISHU'
  return {
    id: null, name: '', messageId: preferredMessageId(messagesForPushChannel(messages, channel)), channel,
    botAppId: bots[0]?.id ?? '', receiveIdType: 'chat_id', receiveId: '',
    webdavUrl: DEFAULT_WEBDAV_URL, webdavUsername: '', webdavPassword: '', webdavPath: '/', hasWebdavPassword: false,
    enabled: true, lockVersion: 0,
  }
}

export function targetDraftFrom(target: OfficePushTarget): PushTargetDraft {
  return {
    id: target.id, name: target.name, messageId: target.messageId, channel: target.channel,
    botAppId: target.botAppId, receiveIdType: target.receiveIdType || 'chat_id', receiveId: target.receiveId,
    webdavUrl: target.webdavUrl || DEFAULT_WEBDAV_URL, webdavUsername: target.webdavUsername,
    webdavPassword: '', webdavPath: target.webdavPath || '/', hasWebdavPassword: target.hasWebdavPassword,
    enabled: target.enabled, lockVersion: target.lockVersion,
  }
}

export function messagesForPushChannel(messages: OfficeMessage[], channel: OfficePushChannel) {
  return channel === 'WEBDAV' ? messages.filter((message) => message.sourceType !== 'EDITED') : messages
}

export function preferredMessageId(messages: OfficeMessage[]) {
  return messages.find((message) => message.enabled)?.id ?? messages[0]?.id ?? 0
}

export function buildPushTargetPayload(draft: PushTargetDraft, messages: OfficeMessage[]) {
  const name = draft.name.trim()
  const message = messages.find((item) => item.id === draft.messageId)
  if (!name || !message) throw new Error('请填写配置名称并选择消息。')
  if (draft.channel === 'FEISHU') {
    if (!draft.botAppId || !draft.receiveId.trim()) throw new Error('请选择飞书机器人并填写接收 ID。')
  } else {
    if (message.sourceType === 'EDITED') throw new Error('WebDAV 只能推送 Excel 消息。')
    if (!draft.webdavUrl.trim() || !draft.webdavUsername.trim() || !draft.webdavPath.trim()) throw new Error('请完整填写 WebDAV URL、账号和固定目录。')
    if (!isValidWebDAVPath(draft.webdavPath)) throw new Error('固定目录必须以 / 开头，且不能包含反斜杠、控制字符或 .、.. 路径段，例如 /商业分析部/测试文件夹。')
    if (!draft.webdavPassword && (!draft.id || !draft.hasWebdavPassword)) throw new Error('请填写 WebDAV 应用密码。')
  }
  return {
    name, messageId: draft.messageId, channel: draft.channel,
    botAppId: draft.channel === 'FEISHU' ? draft.botAppId : '',
    receiveIdType: draft.channel === 'FEISHU' ? draft.receiveIdType : '',
    receiveId: draft.channel === 'FEISHU' ? draft.receiveId.trim() : '',
    webdavUrl: draft.channel === 'WEBDAV' ? draft.webdavUrl.trim() : '',
    webdavUsername: draft.channel === 'WEBDAV' ? draft.webdavUsername.trim() : '',
    webdavPassword: draft.channel === 'WEBDAV' ? draft.webdavPassword : '',
    webdavPath: draft.channel === 'WEBDAV' ? draft.webdavPath.trim() : '',
    enabled: draft.enabled, expectedLockVersion: draft.id ? draft.lockVersion : 0,
  }
}
