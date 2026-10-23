import type { ProcedureConfig, ProcedureParameter } from './excelOperations'

export type OracleProcedureSummary = { owner: string; packageName: string; name: string; overload: string; argumentCount: number }
export type OracleProcedureArgument = { name: string; position: number; direction: ProcedureParameter['direction']; dataType: string; defaulted: boolean }
export type OracleProcedureRef = Pick<OracleProcedureSummary, 'owner' | 'packageName' | 'name'>

function record(value: unknown): Record<string, unknown> | null {
 return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
}

function items(payload: unknown): unknown[] {
 const root = record(payload)
 const data = record(root?.data) ?? root
 if (!Array.isArray(data?.items)) throw new Error('Oracle 存储过程目录响应不完整，请重试。')
 return data.items
}

export function parseOracleProcedures(payload: unknown): OracleProcedureSummary[] {
 return items(payload).map(value => {
  const item = record(value)
  if (!item || typeof item.owner !== 'string' || !item.owner || typeof item.packageName !== 'string' || typeof item.name !== 'string' || !item.name || typeof item.overload !== 'string' || typeof item.argumentCount !== 'number') throw new Error('Oracle 存储过程目录格式不完整，请重试。')
  return { owner: item.owner, packageName: item.packageName, name: item.name, overload: item.overload, argumentCount: item.argumentCount }
 })
}

export function parseOracleProcedureArguments(payload: unknown): OracleProcedureArgument[] {
 return items(payload).map((value): OracleProcedureArgument => {
  const item = record(value)
  const direction = typeof item?.direction === 'string' ? item.direction.toUpperCase().replace(/\//g, ' ').replace(/\s+/g, ' ').trim().replace(/^INOUT$/, 'IN OUT') : ''
  if (!item || typeof item.name !== 'string' || !item.name || typeof item.position !== 'number' || (direction !== 'IN' && direction !== 'OUT' && direction !== 'IN OUT') || typeof item.dataType !== 'string' || !item.dataType || typeof item.defaulted !== 'boolean') throw new Error('Oracle 存储过程参数格式不完整，请重试。')
  return { name: item.name, position: item.position, direction, dataType: item.dataType, defaulted: item.defaulted }
 }).sort((left, right) => left.position - right.position)
}

export function applyOracleProcedureSignature(config: ProcedureConfig, procedure: OracleProcedureRef, argumentsList: readonly OracleProcedureArgument[]): ProcedureConfig {
 const sameProcedure = config.owner.trim().toUpperCase() === procedure.owner.toUpperCase() && config.packageName.trim().toUpperCase() === procedure.packageName.toUpperCase() && config.procedureName.trim().toUpperCase() === procedure.name.toUpperCase()
 const existing = new Map((sameProcedure ? config.parameters : []).map(parameter => [parameter.name.trim().toUpperCase(), parameter]))
 const parameters = argumentsList.flatMap(argument => {
  const previous = existing.get(argument.name.toUpperCase())
  if (argument.direction === 'IN' && argument.defaulted && !previous) return []
  const preserveValue = previous?.direction === argument.direction && previous.dataType.toUpperCase() === argument.dataType.toUpperCase() && argument.direction !== 'OUT'
  return [{ name: argument.name, direction: argument.direction, dataType: argument.dataType, value: preserveValue ? previous.value : '' }]
 })
 const keepSuccess = sameProcedure && parameters.some(parameter => parameter.name === config.success.parameter && parameter.direction !== 'IN')
 return { ...config, owner: procedure.owner, packageName: procedure.packageName, procedureName: procedure.name, parameters, success: keepSuccess ? config.success : { parameter: '', operator: 'eq', value: '' } }
}
