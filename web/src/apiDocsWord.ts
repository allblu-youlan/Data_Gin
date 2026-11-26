import { BorderStyle, Document, HeadingLevel, Packer, Paragraph, Table, TableCell, TableLayoutType, TableRow, TextRun, WidthType } from 'docx'
import { buildApiMarkdown, type ApiDocument } from './apiDocs'

const pageWidth = 11906
const pageHeight = 16838
const pageMargin = 1134
const contentWidth = pageWidth - pageMargin * 2
const bodyFont = { ascii: 'Arial', hAnsi: 'Arial', eastAsia: 'Microsoft YaHei' }
const codeFont = { ascii: 'Consolas', hAnsi: 'Consolas', eastAsia: 'Microsoft YaHei' }

function plainText(value: string): string {
  return value.replace(/<br\s*\/?\s*>/gi, '\n').replace(/\\([\\|`])/g, '$1')
    .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&')
}

function tableCells(line: string): string[] {
  const cells: string[] = []
  let cell = ''
  const content = line.trim().slice(1, -1)
  for (let index = 0; index < content.length; index += 1) {
    const char = content[index]
    if (char === '\\' && index + 1 < content.length) {
      cell += char + content[index + 1]
      index += 1
    } else if (char === '|') {
      cells.push(cell.trim())
      cell = ''
    } else cell += char
  }
  cells.push(cell.trim())
  return cells
}

function wordTable(rows: string[][]): Table {
  const fieldTable = plainText(rows[0][0]) === '字段'
  const widths = fieldTable ? [2800, 1400, contentWidth - 4200] : [900, 1500, contentWidth - 2400]
  const border = { style: BorderStyle.SINGLE, size: 4, color: 'D1D5DB' }
  const noBorder = { style: BorderStyle.NONE, size: 0, color: 'FFFFFF' }
  return new Table({
    width: { size: contentWidth, type: WidthType.DXA },
    columnWidths: widths,
    layout: TableLayoutType.FIXED,
    borders: { top: border, bottom: border, insideHorizontal: border, insideVertical: noBorder, left: noBorder, right: noBorder },
    rows: rows.map((row, index) => new TableRow({
      tableHeader: index === 0,
      cantSplit: index === 0,
      children: row.map((cell, column) => new TableCell({
        width: { size: widths[column], type: WidthType.DXA },
        margins: { top: 90, bottom: 90, left: 120, right: 120 },
        shading: index === 0 ? { fill: 'F3F4F6' } : undefined,
        children: plainText(cell).split('\n').map(text => new Paragraph({
          wordWrap: true,
          keepNext: index === 0,
          spacing: { after: 40, line: 260 },
          children: [new TextRun({ text, bold: index === 0, font: fieldTable && column < 2 && index > 0 ? codeFont : bodyFont, size: 20, color: '000000' })],
        })),
      })),
    })),
  })
}

function wordContent(markdown: string): (Paragraph | Table)[] {
  // This handles the controlled Markdown emitted by buildApiMarkdown, not arbitrary Markdown.
  const children: (Paragraph | Table)[] = []
  const lines = markdown.split('\n')
  const headings = [HeadingLevel.TITLE, HeadingLevel.HEADING_1, HeadingLevel.HEADING_2, HeadingLevel.HEADING_3, HeadingLevel.HEADING_4, HeadingLevel.HEADING_5]
  let inNote = false
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index]
    const fence = line.match(/^(`{3,})\w*$/)
    if (fence) {
      for (index += 1; index < lines.length && lines[index] !== fence[1]; index += 1) {
        children.push(new Paragraph({ style: 'ApiCode', text: lines[index], wordWrap: true }))
      }
      children.push(new Paragraph({ spacing: { after: 100 }, text: '' }))
      continue
    }
    if (line.trim() === '<details>') { inNote = true; continue }
    if (line.trim() === '</details>') { inNote = false; continue }
    if (!line.trim()) continue
    const summary = line.match(/^<summary>(.*)<\/summary>$/)
    if (summary) {
      children.push(new Paragraph({ keepNext: true, spacing: { before: 160, after: 80 }, children: [new TextRun({ text: plainText(summary[1]), bold: true })] }))
      continue
    }
    if (inNote) {
      children.push(new Paragraph({ text: line, wordWrap: true }))
      continue
    }
    const heading = line.match(/^(#{1,6}) (.+)$/)
    if (heading) {
      children.push(new Paragraph({ heading: headings[heading[1].length - 1], text: plainText(heading[2]), keepNext: true, keepLines: true, wordWrap: true }))
      continue
    }
    if (line.startsWith('|') && lines[index + 1]?.startsWith('|') && tableCells(lines[index + 1]).every(cell => /^:?-{3,}:?$/.test(cell))) {
      const rows = [tableCells(line)]
      index += 2
      while (index < lines.length && lines[index].startsWith('|')) {
        rows.push(tableCells(lines[index]))
        index += 1
      }
      index -= 1
      children.push(wordTable(rows), new Paragraph({ spacing: { after: 100 }, text: '' }))
      continue
    }
    for (const text of plainText(line).split('\n')) children.push(new Paragraph({ text, wordWrap: true }))
  }
  return children
}

export async function buildApiWord(document: ApiDocument, baseURL: string, operationId?: string): Promise<Blob> {
  const markdown = buildApiMarkdown(document, baseURL, operationId)
  const heading = (size: number) => ({ run: { font: bodyFont, size, bold: true, color: '000000' }, paragraph: { keepNext: true, keepLines: true, spacing: { before: 240, after: 120 } } })
  const wordDocument = new Document({
    title: document.title,
    creator: 'allblu',
    styles: {
      default: {
        document: { run: { font: bodyFont, size: 22, color: '000000' }, paragraph: { spacing: { after: 100, line: 280 } } },
        title: heading(40),
        heading1: heading(32),
        heading2: heading(28),
        heading3: heading(25),
        heading4: heading(22),
        heading5: heading(22),
      },
      paragraphStyles: [{ id: 'ApiCode', name: 'API code', basedOn: 'Normal', run: { font: codeFont, size: 20, color: '000000' }, paragraph: { spacing: { after: 0, line: 250 } } }],
    },
    sections: [{ properties: { page: { size: { width: pageWidth, height: pageHeight }, margin: { top: pageMargin, right: pageMargin, bottom: pageMargin, left: pageMargin } } }, children: wordContent(markdown) }],
  })
  return Packer.toBlob(wordDocument)
}
