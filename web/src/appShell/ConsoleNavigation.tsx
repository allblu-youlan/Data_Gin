import { useEffect, useState } from 'react'
import { ChevronDown, LogOut, RefreshCcw, Search, X } from 'lucide-react'
import { Brand } from '../components/Brand'
import { navGroupFor, navGroups, visibleNavigationGroups, type NavKey } from './navigation'
import styles from './ConsoleNavigation.module.css'

interface ConsoleNavigationProps {
  activeNav: NavKey
  mobileOpen: boolean
  permissions: readonly string[]
  refreshing: boolean
  onLogout: () => void
  onNavigate: (key: NavKey) => void
  onRefresh: () => void
  onToggleMobile: () => void
}

export function ConsoleNavigation({
  activeNav,
  mobileOpen,
  permissions,
  refreshing,
  onLogout,
  onNavigate,
  onRefresh,
  onToggleMobile,
}: ConsoleNavigationProps) {
  const [expandedGroup, setExpandedGroup] = useState(() => navGroupFor(activeNav)?.label ?? navGroups[0].label)
  const [query, setQuery] = useState('')
  const [desktopNavigation, setDesktopNavigation] = useState(() => window.matchMedia('(min-width: 841px)').matches)
  const visibleGroups = visibleNavigationGroups(permissions)

  useEffect(() => {
    setExpandedGroup(navGroupFor(activeNav)?.label ?? navGroups[0].label)
  }, [activeNav])

  useEffect(() => {
    const viewport = window.matchMedia('(min-width: 841px)')
    const handleChange = () => setDesktopNavigation(viewport.matches)
    viewport.addEventListener('change', handleChange)
    return () => viewport.removeEventListener('change', handleChange)
  }, [])

  return (
    <>
      <Brand className={styles.brand} size="medium" />
      <button
        className={styles.mobileToggle}
        type="button"
        aria-expanded={mobileOpen}
        aria-controls="primary-navigation"
        onClick={onToggleMobile}
      >
        <X aria-hidden="true" />
        关闭菜单
      </button>
      <label className={styles.search}>
        <span>查找页面</span>
        <span className={styles.searchControl}>
          <Search aria-hidden="true" />
          <input
            name="moduleNavigationSearch"
            value={query}
            onChange={(event) => setQuery(event.currentTarget.value)}
            placeholder="输入页面名称或用途"
          />
        </span>
      </label>
      <nav className={styles.navigation} id="primary-navigation">
        {visibleGroups.map((group) => {
          const normalizedQuery = query.trim().toLowerCase()
          const items = group.items.filter((item) => !normalizedQuery || `${item.label} ${item.description}`.toLowerCase().includes(normalizedQuery))
          if (items.length === 0) return null
          const expanded = desktopNavigation || Boolean(normalizedQuery) || expandedGroup === group.label
          const panelID = `nav-group-${group.items[0].key}`
          return (
            <section className={styles.group} key={group.label}>
              <h2>
                <button
                  className={styles.groupToggle}
                  type="button"
                  aria-expanded={expanded}
                  aria-controls={panelID}
                  aria-disabled={desktopNavigation}
                  tabIndex={desktopNavigation ? -1 : undefined}
                  onClick={() => {
                    if (!desktopNavigation) setExpandedGroup((current) => current === group.label ? '' : group.label)
                  }}
                >
                  <span>{group.label}</span>
                  <ChevronDown aria-hidden="true" />
                </button>
              </h2>
              <div className={styles.groupItems} id={panelID} hidden={!expanded}>
                {items.map((item) => (
                  <button
                    className={`${styles.item} ${item.key === activeNav ? styles.active : ''}`}
                    data-nav-active={item.key === activeNav || undefined}
                    key={item.key}
                    type="button"
                    onClick={() => onNavigate(item.key)}
                  >
                    {item.icon}
                    <span><strong>{item.label}</strong><small>{item.description}</small></span>
                  </button>
                ))}
              </div>
            </section>
          )
        })}
      </nav>
      <div className={styles.actions}>
        <button type="button" onClick={onRefresh} disabled={refreshing}>
          <RefreshCcw aria-hidden="true" />
          刷新
        </button>
        <button type="button" onClick={onLogout}>
          <LogOut aria-hidden="true" />
          退出
        </button>
      </div>
    </>
  )
}
