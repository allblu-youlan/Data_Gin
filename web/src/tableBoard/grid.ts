import { columnName } from './clipboard.js'
import type { BoardCell, BoardLayout, BoardSection } from './types'

export const maxSections = 20
export const maxRows = 100
export const maxColumns = 32
export const maxCells = 10000
export const maxTextLength = 2000
export const blankCell = (): BoardCell => ({ kind: 'text', text: '' })

export function gridWidth(section: BoardSection) {
  return section.cells.reduce((max, row) => Math.max(max, row.length), 0)
}

export function resizedCells(section: BoardSection, rows: number, columns: number): BoardCell[][] {
  return Array.from({ length: rows }, (_, r) =>
    Array.from({ length: columns }, (_, c) => section.cells[r]?.[c] ?? blankCell()),
  )
}

export function checkGridSize(layout: BoardLayout, sectionId: string, rows: number, columns: number) {
  if (!Number.isInteger(rows) || rows < 1 || rows > maxRows || !Number.isInteger(columns) || columns < 1 || columns > maxColumns) {
    throw new Error(`单个区块最多容纳 ${maxRows} 行、${maxColumns} 列。`)
  }
  const otherCells = layout.sections.filter(section => section.id !== sectionId)
    .reduce((sum, section) => sum + section.cells.reduce((size, row) => size + row.length, 0), 0)
  if (otherCells + rows * columns > maxCells) throw new Error(`单个看板最多容纳 ${maxCells} 个单元格。`)
}

// Growing the block preserves all surrounding cells. Validation precedes mutation.
export function writeBlock(layout: BoardLayout, section: BoardSection, row: number, column: number, block: BoardCell[][]): BoardSection {
  if (!block.length || !block.some(line => line.length)) throw new Error('请先输入单元格内容。')
  const rows = Math.max(section.cells.length, row + block.length)
  const columns = block.reduce((max, line) => Math.max(max, column + line.length), gridWidth(section))
  checkGridSize(layout, section.id, rows, columns)
  if (block.some(line => line.some(cell => cell.kind === 'text' && Array.from(cell.text).length > maxTextLength))) {
    throw new Error(`每格文本最多 ${maxTextLength} 个字符。`)
  }
  const cells = resizedCells(section, rows, columns)
  block.forEach((line, r) => line.forEach((cell, c) => { cells[row + r][column + c] = cell }))
  return { ...section, cells }
}

export type CopiedRange = { cells: BoardCell[][]; row: number; column: number }

// Shift Excel-style relative coordinates before writing any part of the copied range.
export function pasteRange(layout: BoardLayout, section: BoardSection, row: number, column: number, source: CopiedRange) {
  const rows = Math.max(section.cells.length, row + source.cells.length)
  const columns = Math.max(gridWidth(section), ...source.cells.map(line => column + line.length))
  checkGridSize(layout, section.id, rows, columns)
  const rowDelta = row - source.row
  const columnDelta = column - source.column
  const block = source.cells.map((line, r) => line.map((cell, c) => {
    if (cell.kind !== 'expression') return cell
    const expression = cell.expression.replace(/"[^"]*"|(?<![A-Z0-9_])(\$?)([A-Z]+)(\$?)(\d+)/gi,
      (reference: string, fixedColumn: string, letters: string, fixedRow: string, digits: string) => {
        if (reference.startsWith('"')) return reference
        let x = 0
        for (const letter of letters.toUpperCase()) x = x * 26 + letter.charCodeAt(0) - 64
        x += fixedColumn ? 0 : columnDelta
        const y = Number(digits) + (fixedRow ? 0 : rowDelta)
        if (x < 1 || x > columns || y < 1 || y > rows) {
          throw new Error('粘贴到 ' + columnName(column+c) + (row+r+1) + ' 后，引用 ' + reference + ' 超出目标区块。')
        }
        return fixedColumn + columnName(x-1) + fixedRow + y
      })
    return { ...cell, expression }
  }))
  return writeBlock(layout, section, row, column, block)
}
