// Excel quotes fields that contain tabs, line breaks or double quotes.
export function parseClipboardTable(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let value = ''
  let quoted = false
  const input = text.replace(/\r\n?/g, '\n')

  for (let i = 0; i < input.length; i += 1) {
    const char = input[i]
    if (char === '"' && quoted) {
      if (input[i + 1] === '"') {
        value += '"'
        i += 1
      } else {
        quoted = false
      }
    } else if (char === '"' && !value && input.indexOf('"', i + 1) >= 0) {
      quoted = true
    } else if (!quoted && (char === '\t' || char === '\n')) {
      row.push(value)
      value = ''
      if (char === '\n') {
        rows.push(row)
        row = []
      }
    } else {
      value += char
    }
  }
  if (row.length || value || !input.endsWith('\n')) rows.push([...row, value])
  return rows
}

export function columnName(index: number): string {
  let label = ''
  for (let n = index + 1; n > 0; n = Math.floor((n - 1) / 26)) {
    label = String.fromCharCode(65 + (n - 1) % 26) + label
  }
  return label
}
