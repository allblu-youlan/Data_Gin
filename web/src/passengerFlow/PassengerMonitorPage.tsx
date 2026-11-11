import { useId, useRef, useState, type KeyboardEvent } from 'react'
import type { WorkspaceApiClient } from '../appShell/WorkspaceRouter'
import { PassengerFlowPage } from './PassengerFlowPage'
import { RegionStayPage } from '../regionStay/RegionStayPage'
import styles from './PassengerMonitorPage.module.css'

export function PassengerMonitorPage(props: { client: WorkspaceApiClient; canManage?: boolean }) {
  const [active, setActive] = useState(0)
  const tabs = useRef<Array<HTMLButtonElement | null>>([])
  const id = useId()
  function navigate(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? 1 : event.key === 'ArrowRight' || event.key === 'ArrowLeft' ? 1 - index : null
    if (next === null) return
    event.preventDefault(); setActive(next); tabs.current[next]?.focus()
  }
  return <div className={styles.page}>
    <div className={styles.tabs} role="tablist" aria-label="监控数据类型">
      {['门店客流（小时）', '店内区域（日）'].map((label, index) => <button key={label} type="button" role="tab" id={`${id}-tab-${index}`} aria-controls={`${id}-panel`} aria-selected={active === index} tabIndex={active === index ? 0 : -1} ref={element => { tabs.current[index] = element }} onClick={() => setActive(index)} onKeyDown={event => navigate(event, index)}>{label}</button>)}
    </div>
    <div id={`${id}-panel`} role="tabpanel" aria-labelledby={`${id}-tab-${active}`} tabIndex={0}>
      {active === 0 ? <PassengerFlowPage {...props} /> : <RegionStayPage {...props} />}
    </div>
  </div>
}
