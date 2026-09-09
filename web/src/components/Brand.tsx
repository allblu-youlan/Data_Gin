import type { HTMLAttributes } from 'react'
import styles from './Brand.module.css'

export type BrandSize = 'small' | 'medium' | 'large'

export interface BrandProps extends Omit<HTMLAttributes<HTMLDivElement>, 'children'> {
  /** Applies a stable size variant for the consuming layout to style. */
  size?: BrandSize
  /** Marks the compact layout variant without changing the accessible brand name. */
  compact?: boolean
  /** Allows the logo alternative text to match the surrounding context. */
  logoAlt?: 'Allblu' | 'Allblu Logo'
}

export function Brand({
  className,
  compact = false,
  logoAlt = 'Allblu Logo',
  size = 'medium',
  ...props
}: BrandProps) {
  const classes = [styles.brand, styles[size], compact && styles.compact, className]
    .filter(Boolean)
    .join(' ')

  return (
    <div className={classes} data-compact={compact || undefined} {...props}>
      <img className={styles.logo} src="/allblu-logo-9772212d.jpg" alt={logoAlt} />
    </div>
  )
}
