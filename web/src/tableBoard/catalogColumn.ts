export type CatalogColumn = { name: string; oracleType: string; id?: number; description?: string }
export const numeric = (c: CatalogColumn) => /^(NUMBER|INTEGER|DECIMAL|NUMERIC|FLOAT|BINARY_FLOAT|BINARY_DOUBLE)(\b|\()/i.test(c.oracleType)
export const date = (c: CatalogColumn) => /^(DATE|TIMESTAMP)(\b|\()/i.test(c.oracleType)
export const businessDay = (c: CatalogColumn) => numeric(c) || /^(N?VARCHAR2?|N?CHAR)(\b|\()/i.test(c.oracleType)
