export type NotificationDatasource = { id: number; name: string }
export type NotificationTable = { owner: string; name: string; columnCount: number }
export type NotificationColumn = { name: string; comment: string; dataType: string; nullable: boolean; position: number }

export type NotificationDraft = {
  id: number | null
  name: string
  datasourceId: number
  owner: string
  tableName: string
  cursorColumn: string
  recipientsTemplate: string
  subjectTemplate: string
  bodyTemplate: string
  enabled: boolean
  pollIntervalSeconds: number
  lockVersion: number
}

export type NotificationRule = Omit<NotificationDraft, 'id'> & {
  id: number
  cursorValue: string
  lastCheckedAt: string | null
  lastError: string
  createdAt: string
  updatedAt: string
}

export type NotificationPreview = { recipients: string[]; subject: string; body: string }
export type NotificationDelivery = NotificationPreview & {
  id: number
  ruleId: number
  sourceValue: string
  status: 'queued' | 'retrying' | 'sent' | 'failed' | 'cancelled'
  attempts: number
  lastError: string
  createdAt: string
  sentAt: string | null
}

export type TemplateTarget = 'recipientsTemplate' | 'subjectTemplate' | 'bodyTemplate'
