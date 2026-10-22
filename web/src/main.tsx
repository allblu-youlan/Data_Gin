import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource-variable/noto-sans-sc/wght.css'
import '@fontsource-variable/ibm-plex-sans/wght.css'
import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/latin-500.css'
import '@fontsource/ibm-plex-mono/latin-600.css'
import '@fontsource/ibm-plex-mono/latin-700.css'
import './index.css'
import './styles/shadcn.css'
import App from './App.tsx'
import { AppErrorBoundary } from './appShell/AppErrorBoundary'
import { AppTheme } from './ui/AppTheme/AppTheme'
import controlStyles from './ui/Controls/Controls.module.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <div className={controlStyles.surface}>
      <AppTheme>
        <AppErrorBoundary>
          <App />
        </AppErrorBoundary>
      </AppTheme>
    </div>
  </StrictMode>,
)
