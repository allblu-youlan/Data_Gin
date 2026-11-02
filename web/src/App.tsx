import styles from './App.module.css'
import { ConsoleHeader } from './appShell/ConsoleHeader'
import { ConsoleNavigation } from './appShell/ConsoleNavigation'
import { consoleNavigationClassName } from './appShell/consoleNavigationStyles'
import { LoginScreen } from './appShell/LoginScreen'
import { ResultDrawer } from './appShell/ResultDrawer'
import { usesCompactWorkspace } from './appShell/navigation'
import { useConsoleNavigation } from './appShell/useConsoleNavigation'
import { useConsoleSession } from './appShell/useConsoleSession'
import { useWorkspaceData } from './appShell/useWorkspaceData'
import { WorkspaceRouter } from './appShell/WorkspaceRouter'
import { AppShell } from './ui'

const defaultApiBaseURL = import.meta.env.VITE_API_BASE_URL ?? ''

function App() {
  const session = useConsoleSession(defaultApiBaseURL)
  const permissions = session.sessionUser?.permissions ?? []
  const navigation = useConsoleNavigation(session.sessionState, permissions)
  const workspace = useWorkspaceData(navigation.activeNav, session.client, session.token, session.sessionState, permissions, session.setResult)

  function openStepRuns(runID: number) {
    workspace.setStepRunFocusID(runID)
    navigation.navigate('step_runs')
  }

  async function retryDeliveryLog(logID: number) {
    const response = await session.client(`/v1/delivery-logs/${logID}/retry`, { method: 'POST' })
    if (response.ok) await workspace.refresh(false)
  }

  async function fetchSource(sourceID: number) {
    const response = await session.client(`/v1/sources/${sourceID}/fetch`, { method: 'POST' })
    if (response.ok) await workspace.refresh(false)
    return response
  }

  async function testSource(sourceID: number) {
    return session.client(`/v1/sources/${sourceID}/test`, { method: 'POST' })
  }

  if (session.sessionState !== 'authenticated') {
    return <LoginScreen onLogin={session.login} checking={session.sessionState === 'checking'} />
  }

  const compactWorkspace = usesCompactWorkspace(navigation.activeNav)
  const shellNavigation = <ConsoleNavigation
    activeNav={navigation.activeNav}
    mobileOpen={navigation.mobileNavOpen}
    permissions={permissions}
    refreshing={workspace.refreshing}
    onLogout={session.logout}
    onNavigate={navigation.navigate}
    onRefresh={() => void workspace.refresh(true)}
    onToggleMobile={() => navigation.setMobileNavOpen((open) => !open)}
  />

  return <AppShell
    navigation={shellNavigation}
    navigationClassName={consoleNavigationClassName}
    navigationRef={navigation.mobileNavRef}
    navigationOpen={navigation.mobileNavOpen}
    onDismissNavigation={() => navigation.setMobileNavOpen(false)}
    flushWorkspace={compactWorkspace}
    header={navigation.activeNav === 'sales_summary' ? null : <ConsoleHeader compact={compactWorkspace} activeNav={navigation.activeNav} loading={session.loading || workspace.refreshing} sessionUser={session.sessionUser} onOpenNavigation={() => navigation.setMobileNavOpen(true)} onRefresh={() => void workspace.refresh(true)} onLogout={session.logout} refreshing={workspace.refreshing} mobileNavTriggerRef={navigation.mobileNavTriggerRef} />}
    notices={<>
      {session.sessionValidationError ? <div className={styles.notice} role="status" aria-live="polite">{session.sessionValidationError} <button type="button" onClick={session.retrySessionValidation}>重试校验</button></div> : null}
      {workspace.workspaceError ? <div className={styles.notice} role="alert">{workspace.workspaceError} <button type="button" onClick={() => void workspace.refresh(false)} disabled={workspace.refreshing}>重试</button></div> : null}
    </>}
    overlay={<ResultDrawer result={session.result} onClose={() => session.setResult(null)} />}
  >
    <WorkspaceRouter
      activeNav={navigation.activeNav}
      actorID={session.actorID}
      username={session.sessionUser?.nickname || session.sessionUser?.account || ''}
      onLogout={session.logout}
      onOpenNavigation={() => navigation.setMobileNavOpen(true)}
      client={session.client}
      deliveryLogs={workspace.deliveryLogs}
      destinations={workspace.destinations}
      downloadFile={session.downloadFile}
      legacyTasks={workspace.legacyTasks}
      loading={session.loading}
      monitoring={workspace.monitoring}
      monitoringStale={workspace.monitoringStale}
      navigate={navigation.navigate}
      onFetchSource={fetchSource}
      onLoadSteps={openStepRuns}
      onRefresh={() => workspace.refresh(false)}
      onRetryDeliveryLog={retryDeliveryLog}
      onTestSource={testSource}
      overviewTotals={workspace.overviewTotals}
      permissions={permissions}
      pipelines={workspace.pipelines}
      refreshing={workspace.refreshing}
      refreshVersion={workspace.refreshVersion}
      runs={workspace.runs}
      setLoading={session.setLoading}
      setResult={session.setResult}
      setTransformRules={workspace.setTransformRules}
      sources={workspace.sources}
      stepRunFocusID={workspace.stepRunFocusID}
      token={session.token}
      transformRules={workspace.transformRules}
    />
  </AppShell>
}

export default App
