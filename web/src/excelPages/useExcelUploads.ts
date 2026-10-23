import { useCallback, useRef, useState } from 'react'
import type { ApiRequestOptions, ClientResponse, HTTPMethod } from '../api/client'
import { excelChunkSize, readObject, sameExcelFile, type ExcelUploadRef, type ExcelUploadSession, type ExcelUploadSlot } from './excelPageSupport'

export type ExcelPageClientOptions = Omit<ApiRequestOptions, 'method'> & { method?: HTTPMethod; showResult?: boolean; silentLoading?: boolean }
export type ExcelPageClient = (path: string, options?: ExcelPageClientOptions) => Promise<ClientResponse>

function requestErrorMessage(response: ClientResponse, fallback: string) {
  return response.error?.message || fallback
}

export function buildExcelUploadPayload(uploadId: string | readonly string[], config: unknown) {
  const payload = new FormData()
  if (typeof uploadId === 'string') payload.append('uploadId', uploadId)
  else payload.append('uploadIds', JSON.stringify(uploadId))
  payload.append('config', JSON.stringify(config))
  return payload
}

export function useExcelUploads(client: ExcelPageClient) {
  const uploadRefs = useRef<Partial<Record<ExcelUploadSlot, ExcelUploadRef[]>>>({})
  const [uploadProgress, setUploadProgress] = useState('')

  function clearUploadRef(slot: ExcelUploadSlot) {
    delete uploadRefs.current[slot]
    setUploadProgress('')
  }

  const resetUploads = useCallback(() => {
    uploadRefs.current = {}
    setUploadProgress('')
  }, [])

  async function ensureExcelUpload(slot: ExcelUploadSlot, file: File, batchLabel = '') {
    const existing = uploadRefs.current[slot]?.find(ref => sameExcelFile(file, ref))
    if (existing) {
      setUploadProgress(`${batchLabel}已上传：${file.name}`)
      return existing.uploadId
    }

    const totalChunks = Math.ceil(file.size / excelChunkSize)
    setUploadProgress(`${batchLabel}准备上传 ${file.name}，共 ${totalChunks} 个分片`)
    const createResult = await client('/v1/excel-match-jobs/uploads', {
      method: 'POST',
      body: { fileName: file.name, totalChunks },
      showResult: false,
      silentLoading: true,
      retry: false,
    })
    if (!createResult.ok) throw new Error(requestErrorMessage(createResult, '创建分片上传会话失败'))
    const session = readObject<ExcelUploadSession>(createResult, 'upload')
    if (!session?.uploadId) throw new Error('上传会话返回缺少 uploadId')

    for (let index = 0; index < totalChunks; index++) {
      const start = index * excelChunkSize
      const end = Math.min(file.size, start + excelChunkSize)
      const chunkForm = new FormData()
      chunkForm.append('index', String(index))
      chunkForm.append('totalChunks', String(totalChunks))
      chunkForm.append('chunk', file.slice(start, end), `${file.name}.part${index}`)
      setUploadProgress(`${batchLabel}${file.name} · 上传分片 ${index + 1}/${totalChunks}`)
      const chunkResult = await client(`/v1/excel-match-jobs/uploads/${encodeURIComponent(session.uploadId)}/chunks`, {
        method: 'POST',
        body: chunkForm,
        showResult: false,
        silentLoading: true,
        retry: false,
        timeoutMs: 120_000,
      })
      if (!chunkResult.ok) throw new Error(requestErrorMessage(chunkResult, `上传分片 ${index + 1} 失败`))
    }

    setUploadProgress(`${batchLabel}合并 ${file.name} 分片`)
    const completeResult = await client(`/v1/excel-match-jobs/uploads/${encodeURIComponent(session.uploadId)}/complete`, {
      method: 'POST',
      body: { totalChunks },
      showResult: false,
      silentLoading: true,
      retry: false,
    })
    if (!completeResult.ok) throw new Error(requestErrorMessage(completeResult, '合并 Excel 分片失败'))

    const nextRef: ExcelUploadRef = {
      uploadId: session.uploadId,
      fileName: file.name,
      size: file.size,
      lastModified: file.lastModified,
      totalChunks,
    }
    uploadRefs.current[slot] = [...(uploadRefs.current[slot] ?? []), nextRef]
    setUploadProgress(`${batchLabel}上传完成：${file.name}`)
    return session.uploadId
  }

  async function ensureExcelUploads(slot: ExcelUploadSlot, files: readonly File[]) {
    const uploadIds: string[] = []
    for (let index = 0; index < files.length; index++) {
      uploadIds.push(await ensureExcelUpload(slot, files[index], `文件 ${index + 1}/${files.length} · `))
    }
    setUploadProgress(`已准备 ${files.length} 个 Excel 文件，等待后台顺序处理。`)
    return uploadIds
  }

  return { uploadProgress, clearUploadRef, ensureExcelUpload, ensureExcelUploads, resetUploads }
}
