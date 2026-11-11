import { type FormEvent, useEffect, useState } from 'react'
import { classifyAuthResponse } from '../api/auth'
import { apiURL as buildApiURL } from '../apiURL'
import { Brand } from '../components/Brand'
import styles from './LoginScreen.module.css'

const apiBaseURL = import.meta.env.VITE_API_BASE_URL ?? ''
type LoginMode = 'password' | 'phone' | 'reset'

export function LoginScreen({ onLogin, checking }: { onLogin: (token: string) => void; checking: boolean }) {
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [sending, setSending] = useState(false)
  const [mode, setMode] = useState<LoginMode>('password')
  const [phone, setPhone] = useState('')
  const [countdown, setCountdown] = useState(0)

  useEffect(() => {
    if (countdown <= 0) return
    const timer = window.setTimeout(() => setCountdown((value) => Math.max(0, value - 1)), 1000)
    return () => window.clearTimeout(timer)
  }, [countdown])

  function switchMode(nextMode: LoginMode) {
    if (submitting || sending) return
    setMode(nextMode)
    setError('')
    setNotice('')
  }

  async function sendCode(purpose: 'LOGIN' | 'PASSWORD_RESET') {
    if (sending || countdown > 0) return
    if (!/^1[3-9]\d{9}$/.test(phone.trim())) {
      setError('请输入正确的中国大陆手机号。')
      return
    }
    setSending(true)
    setError('')
    setNotice('')
    try {
      const response = await fetch(apiURL('/auth/phone-codes'), {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone: phone.trim(), purpose }),
      })
      const data: unknown = await response.json().catch(() => null)
      const result = classifyAuthResponse(response.ok, response.status, data)
      if (!result.successful) {
        setError(loginFailureMessage(result.status, 'code'))
        return
      }
      setCountdown(60)
      setNotice('若该手机号对应可用账号，验证码将发送至手机。')
    } catch {
      setError('无法连接认证服务，请检查网络后重试。')
    } finally {
      setSending(false)
    }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError('')
    const form = new FormData(event.currentTarget)
    try {
      const path = mode === 'password' ? '/auth/login/password' : mode === 'phone' ? '/auth/login/phone-code' : '/auth/password/reset'
      const body = mode === 'password'
        ? { account: formValue(form, 'account'), password: formValue(form, 'password') }
        : mode === 'phone'
          ? { phone: phone.trim(), code: formValue(form, 'code') }
          : { phone: phone.trim(), code: formValue(form, 'code'), password: formValue(form, 'newPassword') }
      const response = await fetch(apiURL(path), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data: unknown = await response.json().catch(() => null)
      const result = classifyAuthResponse(response.ok, response.status, data)
      if (mode === 'reset' && result.successful) {
        setMode('password')
        setNotice('密码已重置，请使用新密码登录。')
        setCountdown(0)
        return
      }
      if (!result.token) {
        setError(loginFailureMessage(result.status, mode))
        return
      }
      onLogin(result.token)
    } catch {
      setError('无法连接登录服务，请检查后端服务或代理配置。')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className={styles.shell}>
      <section className={styles.panel}>
        <div className={styles.title}><Brand size="large" /></div>
        <form className={styles.form} onSubmit={submit}>
          <h1>管理员登录</h1>
          {mode !== 'reset' ? (
            <div className={styles.tabs} role="tablist" aria-label="登录方式">
              <button type="button" role="tab" aria-selected={mode === 'password'} className={mode === 'password' ? styles.active : ''} onClick={() => switchMode('password')}>密码登录</button>
              <button type="button" role="tab" aria-selected={mode === 'phone'} className={mode === 'phone' ? styles.active : ''} onClick={() => switchMode('phone')}>验证码登录</button>
            </div>
          ) : null}
          {mode === 'password' ? (
            <>
              <Field label="账号或手机号" name="account" required autoComplete="username" />
              <Field label="密码" name="password" type="password" required autoComplete="current-password" />
              <button className={styles.link} type="button" onClick={() => switchMode('reset')}>忘记密码</button>
            </>
          ) : (
            <>
              <label>手机号
                <input name="phone" inputMode="numeric" autoComplete="tel" value={phone} onChange={(event) => setPhone(event.target.value)} required pattern="1[3-9][0-9]{9}" />
              </label>
              <div className={styles.codeRow}>
                <Field label="短信验证码" name="code" required inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" />
                <button type="button" disabled={sending || countdown > 0} onClick={() => void sendCode(mode === 'phone' ? 'LOGIN' : 'PASSWORD_RESET')}>{sending ? '发送中…' : countdown > 0 ? `${countdown} 秒后重发` : '发送验证码'}</button>
              </div>
            </>
          )}
          {mode === 'reset' ? (
            <>
              <Field label="新密码" name="newPassword" type="password" required minLength={10} maxLength={72} autoComplete="new-password" />
              <button className={styles.link} type="button" onClick={() => switchMode('password')}>返回密码登录</button>
            </>
          ) : null}
          {notice ? <div className={styles.notice} role="status" aria-live="polite">{notice}</div> : null}
          {error ? <div className={styles.error} role="alert" aria-live="polite">{error}</div> : null}
          <button className={styles.primary} type="submit" disabled={submitting || checking}>{submitting || checking ? '正在处理…' : mode === 'reset' ? '重置密码' : '登录'}</button>
        </form>
        <a href="?view=api-docs">查看 allblu开放接口文档</a>
      </section>
    </main>
  )
}

interface FieldProps {
  autoComplete?: string
  inputMode?: 'text' | 'numeric' | 'tel' | 'email' | 'decimal' | 'search' | 'url' | 'none'
  label: string
  maxLength?: number
  minLength?: number
  name: string
  pattern?: string
  required?: boolean
  type?: string
}

function Field({ label, name, type = 'text', required = false, ...props }: FieldProps) {
  return <label>{label}<input name={name} type={type} required={required} {...props} /></label>
}

function apiURL(path: string) {
  return buildApiURL(path, apiBaseURL)
}

function formValue(form: FormData, key: string) {
  const value = form.get(key)
  return typeof value === 'string' ? value : ''
}

function loginFailureMessage(status: number, mode: LoginMode | 'code' = 'password') {
  if (status === 401) return mode === 'password' ? '账号或密码不正确，请重试。' : '手机号或验证码无效，请重试。'
  if (status === 429) return '登录尝试过于频繁，请稍后再试。'
  if (status === 503) return mode === 'code' ? '短信服务暂时不可用，密码登录仍可使用。' : '认证服务暂时不可用，请稍后再试。'
  if (status >= 500) return '登录服务暂时不可用，请稍后再试。'
  return '请求未完成，请检查输入后重试。'
}
