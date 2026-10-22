import { useEffect, useRef, useState } from 'react'
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
  const activeItemRef = useRef<HTMLAnchorElement>(null)
  const visibleGroups = visibleNavigationGroups(permissions)
  const normalizedQuery = query.trim().toLowerCase()
  const filteredGroups = visibleGroups.map((group) => ({
    ...group,
    items: group.items.filter((item) => !normalizedQuery || `${item.label} ${item.description} ${group.label}`.toLowerCase().includes(normalizedQuery)),
  })).filter((group) => group.items.length > 0)

  useEffect(() => {
    setExpandedGroup(navGroupFor(activeNav)?.label ?? navGroups[0].label)
  }, [activeNav])

  useEffect(() => {
    activeItemRef.current?.scrollIntoView({ block: 'nearest' })
  }, [activeNav, expandedGroup, mobileOpen])

  return (
    <>
      <div className={styles.identity}>
        <Brand className={styles.brand} size="medium" />
        <span className={styles.workspaceLabel}>数据协同控制台</span>
      </div>
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
        <span className={styles.visuallyHidden}>查找页面</span>
        <span className={styles.searchControl}>
          <Search aria-hidden="true" />
          <input
            name="moduleNavigationSearch"
            type="search"
            autoComplete="off"
            value={query}
            onChange={(event) => setQuery(event.currentTarget.value)}
            placeholder="查找功能…"
          />
        </span>
      </label>
      <nav className={styles.navigation} id="primary-navigation" aria-label="功能模块">
        {filteredGroups.map((group) => {
          const expanded = Boolean(normalizedQuery) || expandedGroup === group.label
          const panelID = `nav-group-${group.items[0].key}`
          return (
            <section className={styles.group} key={group.label}>
              <h2>
                <button
                  className={styles.groupToggle}
                  type="button"
                  aria-expanded={expanded}
                  aria-controls={panelID}
                  onClick={() => setExpandedGroup((current) => current === group.label ? '' : group.label)}
                >
                  <span>{group.label}</span>
                  <ChevronDown aria-hidden="true" />
                </button>
              </h2>
              <div className={styles.groupItems} id={panelID} hidden={!expanded}>
                {group.items.map((item) => (
                  <a
                    ref={item.key === activeNav ? activeItemRef : undefined}
                    className={`${styles.item} ${item.key === activeNav ? styles.active : ''}`}
                    data-nav-active={item.key === activeNav || undefined}
                    aria-current={item.key === activeNav ? 'page' : undefined}
                    title={item.description}
                    key={item.key}
                    href={`#${item.key}`}
                    onClick={(event) => {
                      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
                      event.preventDefault()
                      onNavigate(item.key)
                    }}
                  >
                    {item.icon}
                    <span><strong>{item.label}</strong><small>{item.description}</small></span>
                  </a>
                ))}
              </div>
            </section>
          )
        })}
        {filteredGroups.length === 0 ? <p className={styles.noResults} role="status">没有找到相关功能</p> : null}
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
