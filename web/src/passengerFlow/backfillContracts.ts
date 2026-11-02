export type PassengerMissingMall = {
  mallId: number; mallName: string; shopId: string; missingHours: number; firstMissingHour: string; lastMissingHour: string
}
export type PassengerMissing = {
  startDate: string; endDate: string; timeZone: string; checkedUntil: string
  expectedHours: number; existingHours: number; missingHours: number; unboundMalls: number
  malls: PassengerMissingMall[]
}
export type PassengerBackfillBatch = {
  savedHours: number; sourceMissingHours: number; failedDays: number; failedHours: number
  scannedDays: number; remainingHours: number; nextCursor: string
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}
function dataOf(value: unknown) { return record(value) && record(value.data) ? value.data : null }
function count(value: unknown) { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 }
function timestamp(value: unknown) { return typeof value === 'string' && Number.isFinite(Date.parse(value)) }

export function parsePassengerMissing(payload: unknown): PassengerMissing | null {
  const data = dataOf(payload)
  if (!data || typeof data.startDate !== 'string' || typeof data.endDate !== 'string' || data.timeZone !== 'Asia/Shanghai'
    || !timestamp(data.checkedUntil) || !['expectedHours', 'existingHours', 'missingHours', 'unboundMalls'].every(key => count(data[key]))
    || !Array.isArray(data.malls) || !data.malls.every(mall => record(mall) && count(mall.mallId) && Number(mall.mallId) > 0
      && typeof mall.mallName === 'string' && typeof mall.shopId === 'string' && count(mall.missingHours)
      && timestamp(mall.firstMissingHour) && timestamp(mall.lastMissingHour))) return null
  return data as PassengerMissing
}

export function parsePassengerBackfill(payload: unknown): PassengerBackfillBatch | null {
  const data = dataOf(payload)
  if (!data || !['savedHours', 'sourceMissingHours', 'failedDays', 'failedHours', 'scannedDays', 'remainingHours'].every(key => count(data[key]))
    || typeof data.nextCursor !== 'string') return null
  return data as PassengerBackfillBatch
}

// Each POST is sent once. A stop request allows the current batch to finish.
export async function runPassengerBackfill(
  send: (cursor: string) => Promise<PassengerBackfillBatch>,
  onProgress: (total: PassengerBackfillBatch) => void,
  shouldStop: () => boolean,
) {
  let total: PassengerBackfillBatch = { savedHours: 0, sourceMissingHours: 0, failedDays: 0, failedHours: 0, scannedDays: 0, remainingHours: 0, nextCursor: '' }
  const seen = new Set<string>()
  do {
    const batch = await send(total.nextCursor)
    total = { ...batch, savedHours: total.savedHours + batch.savedHours, sourceMissingHours: total.sourceMissingHours + batch.sourceMissingHours,
      failedDays: total.failedDays + batch.failedDays, failedHours: total.failedHours + batch.failedHours, scannedDays: total.scannedDays + batch.scannedDays }
    onProgress(total)
    if (batch.nextCursor && seen.has(batch.nextCursor)) throw new Error('补拉进度未前进，请重新检查缺失小时。')
    seen.add(batch.nextCursor)
    if (total.nextCursor && !shouldStop()) await new Promise(resolve => setTimeout(resolve, 600))
  } while (total.nextCursor && !shouldStop())
  return total
}
