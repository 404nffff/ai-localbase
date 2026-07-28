import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useAuth } from '../contexts/AuthContext'
import { APP_VERSION } from '../utils/appInfo'
import { formatAuthError } from './auth/authMessages'
import AppIcon from './common/AppIcon'
import ThemeToggle from './common/ThemeToggle'
import '../styles/Login.css'

interface LoginProps {
  checkingConnection?: boolean
}

const getPasswordStrength = (password: string) => {
  if (!password) {
    return { label: '等待输入', tone: 'idle', hint: '至少 8 位，推荐 16 位以上', level: 0 }
  }

  let score = 0
  if (password.length >= 8) score += 1
  if (password.length >= 16) score += 1
  if (/[a-z]/.test(password) && /[A-Z]/.test(password)) score += 1
  if (/\d/.test(password)) score += 1
  if (/[^a-zA-Z0-9]/.test(password)) score += 1

  if (score >= 4) {
    return { label: '强', tone: 'strong', hint: '适合长期使用', level: 4 }
  }
  if (score >= 2) {
    return { label: '可用', tone: 'medium', hint: '可继续增加长度或字符类型', level: 2 }
  }
  return { label: '偏弱', tone: 'weak', hint: '建议增加长度与字符类型', level: 1 }
}

const Login: React.FC<LoginProps> = ({ checkingConnection = false }) => {
  const {
    login,
    setup,
    authError,
    authConnectionError,
    authReady,
    setupRequired,
    setupTokenRequired,
    username: configuredUsername,
    clearAuthError,
    refreshAuthBootstrap,
  } = useAuth()
  const [username, setUsername] = useState(configuredUsername || 'root')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [setupToken, setSetupToken] = useState('')
  const [localError, setLocalError] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [showResetHelp, setShowResetHelp] = useState(false)
  const [capsLockOn, setCapsLockOn] = useState(false)
  const usernameRef = useRef<HTMLInputElement>(null)
  const passwordRef = useRef<HTMLInputElement>(null)
  const resetHelpButtonRef = useRef<HTMLButtonElement>(null)
  const resetDialogCloseRef = useRef<HTMLButtonElement>(null)
  const resetDialogReturnRef = useRef<HTMLButtonElement>(null)
  const passwordStrength = useMemo(() => getPasswordStrength(password), [password])
  const isCheckingAuth = checkingConnection || !authReady
  const authMode = setupRequired ? 'setup' : 'login'
  const displayError = localError || (authError ? formatAuthError(authError, authMode) : '')
  const passwordsMatch = Boolean(confirmPassword) && password === confirmPassword

  useEffect(() => {
    setUsername(configuredUsername || 'root')
  }, [configuredUsername])

  useEffect(() => {
    if (isCheckingAuth || authConnectionError) return
    if (setupRequired) {
      usernameRef.current?.focus()
      return
    }
    passwordRef.current?.focus()
  }, [authConnectionError, isCheckingAuth, setupRequired])

  useEffect(() => {
    if (!showResetHelp) return

    resetDialogCloseRef.current?.focus()
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setShowResetHelp(false)
        requestAnimationFrame(() => resetHelpButtonRef.current?.focus())
        return
      }
      if (event.key !== 'Tab') return
      const firstElement = resetDialogCloseRef.current
      const lastElement = resetDialogReturnRef.current
      if (!firstElement || !lastElement) return
      if (event.shiftKey && document.activeElement === firstElement) {
        event.preventDefault()
        lastElement.focus()
      } else if (!event.shiftKey && document.activeElement === lastElement) {
        event.preventDefault()
        firstElement.focus()
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [showResetHelp])

  const clearErrors = () => {
    if (localError) setLocalError('')
    if (authError) clearAuthError()
  }

  const closeResetHelp = () => {
    setShowResetHelp(false)
    requestAnimationFrame(() => resetHelpButtonRef.current?.focus())
  }

  const handleCapsLock = (event: React.KeyboardEvent<HTMLInputElement>) => {
    setCapsLockOn(event.getModifierState('CapsLock'))
  }

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    setLocalError('')
    clearAuthError()

    if (setupRequired) {
      if (password.length < 8) {
        setLocalError('密码至少需要 8 个字符。')
        return
      }
      if (password !== confirmPassword) {
        setLocalError('两次输入的密码不一致。')
        return
      }
      if (setupTokenRequired && !setupToken.trim()) {
        setLocalError('请输入初始化 Token。')
        return
      }
    }

    setIsLoading(true)
    try {
      if (setupRequired) {
        await setup({ username: username.trim() || 'root', password, setupToken: setupToken.trim() || undefined })
      } else {
        await login(username.trim() || 'root', password)
      }
    } catch {
      requestAnimationFrame(() => {
        passwordRef.current?.focus()
        passwordRef.current?.select()
      })
    } finally {
      setIsLoading(false)
    }
  }

  const submitDisabled = isLoading || !authReady || !password || !username.trim()
    || (setupRequired && (!confirmPassword || (setupTokenRequired && !setupToken.trim())))

  return (
    <main className="login-page">
      <ThemeToggle />

      <section className="login-shell" aria-label="AI LocalBase 身份验证">
        <aside className="login-brand-panel">
          <div className="login-brand-mark">
            <span><AppIcon name="database" size={22} /></span>
            <strong>AI LocalBase</strong>
          </div>
          {APP_VERSION && <span className="login-brand-version">{APP_VERSION}</span>}
        </aside>

        <section className="login-auth-panel">
          <div className="login-auth-card">
            {isCheckingAuth ? (
              <div className="login-auth-state" role="status" aria-live="polite">
                <span className="login-auth-state-icon is-loading"><AppIcon name="database" size={22} /></span>
                <h2>正在连接</h2>
                <div className="login-auth-progress"><span /></div>
              </div>
            ) : authConnectionError ? (
              <div className="login-auth-state" role="alert">
                <span className="login-auth-state-icon is-error"><AppIcon name="alert" size={22} /></span>
                <h2>暂时无法连接</h2>
                <p>{authConnectionError}</p>
                <button className="login-retry-btn" onClick={() => void refreshAuthBootstrap()} type="button">
                  <AppIcon name="refresh" size={16} />
                  重新检查
                </button>
              </div>
            ) : (
              <>
                <header className="login-auth-header">
                  {setupRequired && <span className="login-mode-label is-setup">首次初始化</span>}
                  <h2>{setupRequired ? '创建管理员账户' : '欢迎回来'}</h2>
                  {setupRequired && <p>设置管理员账户后即可进入。</p>}
                </header>

                <form className="login-form" onSubmit={handleSubmit} aria-busy={isLoading}>
                  <div className="login-field">
                    <label className="login-field-label" htmlFor="login-username">用户名</label>
                    <span className="input-wrapper">
                      <AppIcon className="input-icon" name="user" size={17} />
                      <input
                        aria-label="用户名"
                        autoCapitalize="none"
                        autoComplete="username"
                        className="login-input username-field"
                        disabled={isLoading}
                        id="login-username"
                        onChange={(event) => {
                          setUsername(event.target.value)
                          clearErrors()
                        }}
                        placeholder="管理员用户名"
                        ref={usernameRef}
                        required
                        spellCheck={false}
                        type="text"
                        value={username}
                      />
                    </span>
                  </div>

                  <div className="login-field">
                    <div className="login-field-label">
                      <label htmlFor="login-password">密码</label>
                      {!setupRequired && (
                        <button
                          aria-label="忘记管理员密码"
                          onClick={(event) => {
                            event.preventDefault()
                            setShowResetHelp(true)
                          }}
                          ref={resetHelpButtonRef}
                          type="button"
                        >
                          忘记密码？
                        </button>
                      )}
                    </div>
                    <span className="input-wrapper">
                      <AppIcon className="input-icon" name="lock" size={17} />
                      <input
                        aria-invalid={Boolean(displayError)}
                        aria-label="密码"
                        autoComplete={setupRequired ? 'new-password' : 'current-password'}
                        className="login-input password-field"
                        disabled={isLoading}
                        id="login-password"
                        onBlur={() => setCapsLockOn(false)}
                        onChange={(event) => {
                          setPassword(event.target.value)
                          clearErrors()
                        }}
                        onKeyDown={handleCapsLock}
                        onKeyUp={handleCapsLock}
                        placeholder={setupRequired ? '设置管理员密码' : '输入访问密码'}
                        ref={passwordRef}
                        required
                        type={showPassword ? 'text' : 'password'}
                        value={password}
                      />
                      <button
                        aria-label={showPassword ? '隐藏密码' : '显示密码'}
                        className="password-toggle"
                        disabled={isLoading}
                        onClick={(event) => {
                          event.preventDefault()
                          setShowPassword((visible) => !visible)
                        }}
                        title={showPassword ? '隐藏密码' : '显示密码'}
                        type="button"
                      >
                        <AppIcon name={showPassword ? 'eyeOff' : 'eye'} size={16} />
                      </button>
                    </span>
                  </div>

                  {capsLockOn && (
                    <div className="login-inline-note is-warning">
                      <AppIcon name="alert" size={14} />
                      Caps Lock 已开启
                    </div>
                  )}

                  {setupRequired && (
                    <>
                      <div className={`login-password-strength ${passwordStrength.tone}`}>
                        <div className="login-password-strength-head">
                          <span>密码强度</span>
                          <strong>{passwordStrength.label}</strong>
                        </div>
                        <div className="login-password-meter" aria-hidden="true">
                          {[1, 2, 3, 4].map((level) => (
                            <span className={level <= passwordStrength.level ? 'is-active' : ''} key={level} />
                          ))}
                        </div>
                        <p>{passwordStrength.hint}</p>
                      </div>

                      <div className="login-field">
                        <div className="login-field-label">
                          <label htmlFor="login-confirm-password">确认密码</label>
                          {confirmPassword && (
                            <small className={passwordsMatch ? 'is-valid' : 'is-invalid'}>
                              {passwordsMatch ? '密码一致' : '输入不一致'}
                            </small>
                          )}
                        </div>
                        <span className="input-wrapper">
                          <AppIcon className="input-icon" name="check" size={17} />
                          <input
                            aria-invalid={Boolean(confirmPassword) && !passwordsMatch}
                            aria-label="确认密码"
                            autoComplete="new-password"
                            className="login-input"
                            disabled={isLoading}
                            id="login-confirm-password"
                            onChange={(event) => {
                              setConfirmPassword(event.target.value)
                              clearErrors()
                            }}
                            placeholder="再次输入密码"
                            required
                            type={showPassword ? 'text' : 'password'}
                            value={confirmPassword}
                          />
                        </span>
                      </div>

                      {setupTokenRequired && (
                        <div className="login-field">
                          <div className="login-field-label">
                            <label htmlFor="login-setup-token">初始化 Token</label>
                            <small>由部署管理员提供</small>
                          </div>
                          <span className="input-wrapper">
                            <AppIcon className="input-icon" name="key" size={17} />
                            <input
                              aria-label="初始化 Token"
                              autoComplete="off"
                              className="login-input"
                              disabled={isLoading}
                              id="login-setup-token"
                              onChange={(event) => {
                                setSetupToken(event.target.value)
                                clearErrors()
                              }}
                              placeholder="输入初始化 Token"
                              required
                              type="password"
                              value={setupToken}
                            />
                          </span>
                        </div>
                      )}
                    </>
                  )}

                  {displayError && (
                    <div className="login-error" role="alert">
                      <AppIcon name="alert" size={16} />
                      <span>{displayError}</span>
                    </div>
                  )}

                  <button className="login-submit-btn" disabled={submitDisabled} type="submit">
                    {isLoading ? (
                      <>
                        <span className="submit-spinner" />
                        <span>{setupRequired ? '正在创建账户' : '正在验证'}</span>
                      </>
                    ) : (
                      <>
                        <AppIcon name={setupRequired ? 'user' : 'lock'} size={16} />
                        <span>{setupRequired ? '创建账户并进入' : '登录工作区'}</span>
                      </>
                    )}
                  </button>
                </form>
              </>
            )}
          </div>
        </section>
      </section>

      {showResetHelp && (
        <div
          className="login-help-overlay"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) closeResetHelp()
          }}
        >
          <div
            aria-describedby="login-reset-description"
            aria-labelledby="login-reset-title"
            aria-modal="true"
            className="login-help-dialog"
            role="dialog"
          >
            <header className="login-help-header">
              <span className="login-help-icon" aria-hidden="true"><AppIcon name="shield" size={21} /></span>
              <button
                aria-label="关闭密码重置帮助"
                className="login-help-dismiss"
                onClick={closeResetHelp}
                ref={resetDialogCloseRef}
                title="关闭"
                type="button"
              >
                <AppIcon name="x" size={17} />
              </button>
            </header>
            <h2 id="login-reset-title">重置管理员密码</h2>
            <p id="login-reset-description">
              自部署环境不保存可用于找回密码的明文凭据，需要在服务器侧执行一次性重置。
            </p>
            <ol className="login-reset-steps">
              <li>
                <span>1</span>
                <div><strong>生成一次性 Token</strong><code>openssl rand -base64 32</code></div>
              </li>
              <li>
                <span>2</span>
                <div><strong>设置重置变量</strong><code>AUTH_RESET_TOKEN / AUTH_RESET_PASSWORD</code></div>
              </li>
              <li>
                <span>3</span>
                <div><strong>重启并验证登录</strong><code>成功后移除变量并再次重启</code></div>
              </li>
            </ol>
            <button
              className="login-help-close"
              onClick={closeResetHelp}
              ref={resetDialogReturnRef}
              type="button"
            >
              返回登录
            </button>
          </div>
        </div>
      )}
    </main>
  )
}

export default Login
