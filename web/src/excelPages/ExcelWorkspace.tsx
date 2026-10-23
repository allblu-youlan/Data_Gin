import { useId, type HTMLAttributes, type ReactNode } from 'react'
import { PageCanvas, PageHeader } from '../ui'
import { cn } from '@/lib/utils'
import styles from './ExcelWorkspace.module.css'

type WorkspaceProps = Omit<HTMLAttributes<HTMLElement>, 'title'> & {
 title: ReactNode
 description?: ReactNode
 actions?: ReactNode
 children: ReactNode
}

export function ExcelWorkspacePage({ title, description, actions, children }: Pick<WorkspaceProps, 'title' | 'description' | 'actions' | 'children'>) {
 return <PageCanvas className={styles.page} density="compact">
  <PageHeader eyebrow="EXCEL WORKSPACE" title={title} description={description} actions={actions} />
  {children}
 </PageCanvas>
}

export function ExcelWorkspace({ title, description, actions, children, className, ...props }: WorkspaceProps) {
 const titleID = useId()
 return <section className={cn(styles.workspace, className)} aria-labelledby={titleID} {...props}>
  <header className={styles.heading}>
   <div><h2 id={titleID}>{title}</h2>{description && <p>{description}</p>}</div>
   {actions && <div className={styles.actions}>{actions}</div>}
  </header>
  {children}
 </section>
}

export function ExcelEditor({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
 return <div className={cn(styles.editor, className)} {...props} />
}

export function ExcelEditorSection({ title, description, actions, children, className, ...props }: WorkspaceProps) {
 const titleID = useId()
 return <section className={cn(styles.editorSection, className)} aria-labelledby={titleID} {...props}>
  <header className={styles.sectionHeading}>
   <div><h3 id={titleID}>{title}</h3>{description && <p>{description}</p>}</div>
   {actions && <div className={styles.actions}>{actions}</div>}
  </header>
  {children}
 </section>
}
