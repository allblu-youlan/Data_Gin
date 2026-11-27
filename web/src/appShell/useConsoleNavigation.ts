import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { accessibleNavFromHash, navFromHash, type NavKey } from './navigation'
import type { ConsoleSessionState } from './useConsoleSession'

const focusableSelector = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export function useConsoleNavigation(sessionState: ConsoleSessionState, permissions: readonly string[]) {
  const [requestedNav, setRequestedNav] = useState<NavKey>(navFromHash)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const mobileNavTriggerRef = useRef<HTMLButtonElement>(null)
  const mobileNavRef = useRef<HTMLElement>(null)

  useEffect(() => {
    if (!mobileNavOpen) return
    const previousOverflow = document.body.style.overflow
    const navigation = mobileNavRef.current
    document.body.style.overflow = 'hidden'
    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        setMobileNavOpen(false)
        return
      }
      if (event.key !== 'Tab') return
      const items = Array.from(navigation?.querySelectorAll<HTMLElement>(focusableSelector) ?? []).filter((item) => !item.hasAttribute('hidden'))
      if (items.length === 0) return
      const first = items[0]
      const last = items[items.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', handleKeydown)
    navigation?.querySelector<HTMLElement>(focusableSelector)?.focus()
    const mobileNavTrigger = mobileNavTriggerRef.current
    return () => {
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', handleKeydown)
      if (window.matchMedia('(max-width: 840px)').matches) mobileNavTrigger?.focus()
      else (navigation?.querySelector<HTMLElement>('[data-nav-active="true"]') ?? navigation?.querySelector<HTMLElement>(focusableSelector))?.focus()
    }
  }, [mobileNavOpen])

  useLayoutEffect(() => {
    const mobileViewport = window.matchMedia('(max-width: 840px)')
    const syncAccessibility = () => {
      const navigation = mobileNavRef.current
      if (!navigation) return
      const shouldHideNavigation = mobileViewport.matches && !mobileNavOpen
      if (shouldHideNavigation && navigation.contains(document.activeElement)) mobileNavTriggerRef.current?.focus()
      navigation.toggleAttribute('inert', shouldHideNavigation)
      if (shouldHideNavigation) navigation.setAttribute('aria-hidden', 'true')
      else navigation.removeAttribute('aria-hidden')
      if (!mobileViewport.matches) setMobileNavOpen(false)
    }
    syncAccessibility()
    mobileViewport.addEventListener('change', syncAccessibility)
    return () => mobileViewport.removeEventListener('change', syncAccessibility)
  }, [mobileNavOpen, sessionState])

  const activeRequest = useRef(requestedNav)
  activeRequest.current = requestedNav
  const navigate = useCallback((key: NavKey) => {
    const enter = () => {
      activeRequest.current = key
      window.location.hash = key
      setRequestedNav(key)
      setMobileNavOpen(false)
    }
    const leaving = key !== activeRequest.current
    const event = new CustomEvent('console-before-navigate', { cancelable: true, detail: { enter } })
    if (leaving && !window.dispatchEvent(event)) {
      if (navFromHash() !== activeRequest.current) history.replaceState(history.state, '', '#' + activeRequest.current)
      return
    }
    enter()
  }, [])
  useEffect(() => {
    const handleHashChange = () => navigate(navFromHash())
    window.addEventListener('hashchange', handleHashChange)
    return () => window.removeEventListener('hashchange', handleHashChange)
  }, [navigate])

  const accessibleNav = sessionState === 'authenticated' ? accessibleNavFromHash(permissions, `#${requestedNav}`) : requestedNav
  const activeNav = accessibleNav ?? requestedNav

  useEffect(() => {
    if (sessionState !== 'authenticated' || !accessibleNav || accessibleNav === requestedNav) return
    window.location.hash = accessibleNav
    setRequestedNav(accessibleNav)
  }, [accessibleNav, requestedNav, sessionState])

  return { activeNav, mobileNavOpen, mobileNavRef, mobileNavTriggerRef, navigate, setMobileNavOpen }
}
