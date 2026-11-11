import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource-variable/noto-sans-sc/wght.css'
import '@fontsource-variable/ibm-plex-sans/wght.css'
import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/latin-500.css'
import '@fontsource/ibm-plex-mono/latin-600.css'
import '@fontsource/ibm-plex-mono/latin-700.css'
import './index.css'
import './styles/shadcn.css'
import { AppErrorBoundary } from './appShell/AppErrorBoundary'
import { AppTheme } from './ui/AppTheme/AppTheme'
import controlStyles from './ui/Controls/Controls.module.css'

const App = lazy(() => import('./App.tsx'))
const ApiDocsPage = lazy(() => import('./ApiDocsPage'))
const isApiDocs = window.location.pathname === '/api-docs' || new URLSearchParams(window.location.search).get('view') === 'api-docs'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <div className={controlStyles.surface}>
      <AppTheme>
        <AppErrorBoundary>
          <Suspense fallback={<div role="status">正在加载…</div>}>{isApiDocs ? <ApiDocsPage /> : <App />}</Suspense>
        </AppErrorBoundary>
      </AppTheme>
    </div>
  </StrictMode>,
)
