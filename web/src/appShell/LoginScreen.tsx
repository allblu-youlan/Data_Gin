import { type FormEvent, type ReactNode, useEffect, useId, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, BookOpen, Eye, EyeOff } from 'lucide-react'
import { classifyAuthResponse } from '../api/auth'
import { apiURL as buildApiURL } from '../apiURL'
import { Brand } from '../components/Brand'
import { Button } from '../ui/Button/Button'
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
  const headingRef = useRef<HTMLHeadingElement>(null)

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
    if (nextMode === 'reset' || mode === 'reset') {
      window.requestAnimationFrame(() => headingRef.current?.focus())
    }
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
        window.requestAnimationFrame(() => headingRef.current?.focus())
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
      <div className={styles.layout}>
        <div className={styles.brand}>
          <Brand size="medium" />
          <span>数据协同控制台</span>
        </div>
        <section className={styles.panel} aria-labelledby="login-heading">
          <div className={styles.panelBody}>
            {mode === 'reset' ? (
              <Button className={styles.back} disabled={submitting || sending} onClick={() => switchMode('password')}>
                <ArrowLeft size={15} aria-hidden="true" />返回登录
              </Button>
            ) : null}
            <header className={styles.heading}>
              <h1 id="login-heading" ref={headingRef} tabIndex={-1}>{mode === 'reset' ? '重置密码' : '登录控制台'}</h1>
              {mode === 'reset' ? <p>验证绑定的手机号，为账号设置新密码。</p> : null}
            </header>
            {mode !== 'reset' ? (
              <div className={styles.tabs} role="group" aria-label="登录方式">
                <Button aria-pressed={mode === 'password'} disabled={submitting || sending} onClick={() => switchMode('password')}>密码登录</Button>
                <Button aria-pressed={mode === 'phone'} disabled={submitting || sending} onClick={() => switchMode('phone')}>验证码登录</Button>
              </div>
            ) : null}
            <form className={styles.form} onSubmit={submit} aria-label={mode === 'reset' ? '重置密码' : '管理员登录'} aria-busy={submitting || checking}>
              <div className={styles.fields} key={mode}>
                {mode === 'password' ? (
                  <>
                    <Field label="账号或手机号" name="account" placeholder="请输入账号或手机号" required autoComplete="username" />
                    <Field label="密码" name="password" type="password" placeholder="请输入密码" required autoComplete="current-password"
                      action={<Button className={styles.link} disabled={submitting || sending} onClick={() => switchMode('reset')}>忘记密码？</Button>} />
                  </>
                ) : (
                  <>
                    <label className={styles.field}>手机号
                      <input name="phone" type="tel" inputMode="tel" autoComplete="tel" placeholder="请输入绑定的手机号" value={phone} onChange={(event) => setPhone(event.target.value)} required pattern="1[3-9][0-9]{9}" />
                    </label>
                    <div className={styles.codeRow}>
                      <Field label="短信验证码" name="code" placeholder="6 位验证码" required inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" />
                      <Button className={styles.sendCode} disabled={sending || submitting || countdown > 0} onClick={() => void sendCode(mode === 'phone' ? 'LOGIN' : 'PASSWORD_RESET')}>{sending ? '发送中…' : countdown > 0 ? `${countdown} 秒后重发` : '获取验证码'}</Button>
                    </div>
                  </>
                )}
                {mode === 'reset' ? (
                  <Field label="新密码" name="newPassword" type="password" placeholder="请设置新密码" required minLength={10} maxLength={72} autoComplete="new-password" hint="使用 10–72 位字母、数字或符号。" />
                ) : null}
              </div>
              {notice ? <div className={styles.notice} role="status">{notice}</div> : null}
              {error ? <div className={styles.error} role="alert">{error}</div> : null}
              <Button variant="primary" className={styles.primary} type="submit" disabled={submitting || checking || sending}>
                {submitting || checking ? '正在处理…' : mode === 'reset' ? '重置密码' : '登录控制台'}
                {!submitting && !checking ? <ArrowRight size={16} aria-hidden="true" /> : null}
              </Button>
            </form>
          </div>
          <a className={styles.docsLink} href="?view=api-docs">
            <span className={styles.docsIcon}><BookOpen size={19} strokeWidth={1.7} aria-hidden="true" /></span>
            <span className={styles.docsText}>
              <span>allblu开放接口文档</span>
              <small>接口说明与接入指南 · 无需登录</small>
            </span>
            <ArrowRight className={styles.docsArrow} size={18} aria-hidden="true" />
          </a>
        </section>
        <p className={styles.footer}>allblu · 让数据协同更简单</p>
      </div>
    </main>
  )
}

interface FieldProps {
  action?: ReactNode
  autoComplete?: string
  hint?: string
  inputMode?: 'text' | 'numeric' | 'tel' | 'email' | 'decimal' | 'search' | 'url' | 'none'
  label: string
  maxLength?: number
  minLength?: number
  name: string
  pattern?: string
  placeholder?: string
  required?: boolean
  type?: string
}

function Field({ action, hint, label, name, type = 'text', required = false, ...props }: FieldProps) {
  const id = useId()
  const [visible, setVisible] = useState(false)
  const isPassword = type === 'password'
  return (
    <div className={styles.field}>
      <div className={styles.labelRow}>
        <label htmlFor={id}>{label}</label>
        {action}
      </div>
      <div className={styles.inputWrap}>
        <input id={id} name={name} type={isPassword && visible ? 'text' : type} required={required} className={isPassword ? styles.passwordInput : undefined} aria-describedby={hint ? `${id}-hint` : undefined} {...props} />
        {isPassword ? (
          <Button className={styles.passwordToggle} aria-label={visible ? '隐藏密码' : '显示密码'} onClick={() => setVisible((value) => !value)}>
            {visible ? <EyeOff size={17} aria-hidden="true" /> : <Eye size={17} aria-hidden="true" />}
          </Button>
        ) : null}
      </div>
      {hint ? <span className={styles.hint} id={`${id}-hint`}>{hint}</span> : null}
    </div>
  )
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
