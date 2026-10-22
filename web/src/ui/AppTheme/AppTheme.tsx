import ConfigProvider from 'antd/es/config-provider'
import zhCN from 'antd/es/locale/zh_CN'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import type { ThemeConfig } from 'antd/es/config-provider/context'
import type { ReactNode } from 'react'
import { consoleColors } from '../../styles/themeTokens'

dayjs.locale('zh-cn')

const consoleTheme: ThemeConfig = {
  token: {
    colorPrimary: consoleColors.primary,
    colorInfo: consoleColors.info,
    colorSuccess: consoleColors.success,
    colorWarning: consoleColors.warning,
    colorError: consoleColors.danger,
    colorText: consoleColors.text,
    colorTextSecondary: consoleColors.textSecondary,
    colorBorder: consoleColors.border,
    colorBgBase: consoleColors.canvas,
    colorBgLayout: consoleColors.workspace,
    borderRadius: 6,
    borderRadiusLG: 8,
    controlHeight: 40,
    fontFamily: '"IBM Plex Sans Variable", "Noto Sans SC Variable", "PingFang SC", "Microsoft YaHei", sans-serif',
  },
  components: {
    Button: {
      borderRadius: 6,
      controlHeight: 36,
      primaryShadow: 'none',
    },
    Card: {
      borderRadiusLG: 8,
      boxShadowTertiary: 'none',
    },
    Modal: {
      borderRadiusLG: 12,
    },
    Drawer: {
      borderRadiusLG: 0,
    },
    Table: {
      borderRadius: 0,
      borderRadiusLG: 0,
      cellPaddingBlock: 11,
    },
  },
}

export function AppTheme({ children }: { children: ReactNode }) {
  return <ConfigProvider locale={zhCN} theme={consoleTheme}>{children}</ConfigProvider>
}
