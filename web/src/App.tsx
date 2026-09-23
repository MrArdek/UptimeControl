import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useState } from 'react'
import { APIError, api, basePath, mutate } from './api'
import type { Check, Incident, Monitor, Notification, OpenIncident, Project, Summary, User, Webhook } from './types'

type LiveState = 'connecting' | 'live' | 'polling'
type Period = '24h' | '7d' | '30d'

const paths = {
  projects: '/projects',
  settings: '/settings',
  analytics: '/analytics',
  events: '/events',
  properties: '/properties',
  servers: '/servers',
}

function currentPath() {
  const path = window.location.pathname
  const relative = basePath && path.startsWith(basePath) ? path.slice(basePath.length) : path
  return relative || '/'
}

function useRoute() {
  const [path, setPath] = useState(currentPath)
  useEffect(() => {
    const update = () => setPath(currentPath())
    window.addEventListener('popstate', update)
    window.addEventListener('uptime:navigate', update)
    return () => {
      window.removeEventListener('popstate', update)
      window.removeEventListener('uptime:navigate', update)
    }
  }, [])
  return path
}

function navigate(path: string) {
  window.history.pushState({}, '', `${basePath}${path}`)
  window.dispatchEvent(new Event('uptime:navigate'))
}

function Link({ to, children, className = '' }: { to: string; children: ReactNode; className?: string }) {
  return <a className={className} href={`${basePath}${to}`} onClick={(event) => {
    if (event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey) {
      event.preventDefault()
      navigate(to)
    }
  }}>{children}</a>
}

export function App() {
  const [user, setUser] = useState<User | null | undefined>(undefined)
  const [projects, setProjects] = useState<Project[]>([])
  const [incidents, setIncidents] = useState<OpenIncident[]>([])
  const [liveState, setLiveState] = useState<LiveState>('connecting')
  const [lastSnapshot, setLastSnapshot] = useState<string | null>(null)
  const route = useRoute()

  const refreshProjects = useCallback(async () => {
    const result = await api<{ projects: Project[] }>('/projects?limit=500')
    setProjects(result.projects)
    setLastSnapshot(new Date().toISOString())
  }, [])

  useEffect(() => {
    api<{ user: User }>('/auth/me').then(({ user: current }) => setUser(current)).catch(() => setUser(null))
  }, [])

  useEffect(() => {
    if (!user) return
    let source: EventSource | null = null
    let timer = 0
    const poll = () => {
      window.clearInterval(timer)
      setLiveState('polling')
      void refreshProjects().catch((error) => {
        if (error instanceof APIError && error.status === 401) setUser(null)
      })
      timer = window.setInterval(() => {
        if (!document.hidden) void refreshProjects()
      }, 15_000)
    }
    void refreshProjects().catch(() => undefined)
    if (!window.EventSource) {
      poll()
      return () => window.clearInterval(timer)
    }
    source = new EventSource(`${basePath}/api/v1/events`)
    source.addEventListener('snapshot', (event) => {
      const snapshot = JSON.parse((event as MessageEvent).data) as {
        projects: { projects: Project[] }
        incidents: OpenIncident[]
        generated_at: string
      }
      setProjects(snapshot.projects.projects)
      setIncidents(snapshot.incidents)
      setLastSnapshot(snapshot.generated_at)
      setLiveState('live')
      window.clearInterval(timer)
    })
    source.addEventListener('session-expired', () => setUser(null))
    source.addEventListener('stream-error', poll)
    source.onerror = poll
    return () => {
      source?.close()
      window.clearInterval(timer)
    }
  }, [user, refreshProjects])

  async function logout() {
    try { await mutate('/auth/logout', 'POST') } catch { /* local state still ends access */ }
    setUser(null)
    setProjects([])
    setIncidents([])
    navigate('/')
  }

  if (user === undefined) return <FullPageState>Проверяем сессию…</FullPageState>
  if (!user) return <AuthPage onAuthenticated={setUser} />

  const monitorMatch = route.match(/^\/projects\/([0-9a-f-]+)\/monitors\/([0-9a-f-]+)$/)
  const notificationsMatch = route.match(/^\/projects\/([0-9a-f-]+)\/notifications$/)
  let page: ReactNode
  if (monitorMatch) {
    page = <MonitorPage projectID={monitorMatch[1]} monitorID={monitorMatch[2]} projects={projects} />
  } else if (notificationsMatch) {
    page = <NotificationsPage projectID={notificationsMatch[1]} projects={projects} />
  } else if (route === paths.settings) {
    page = <SettingsPage user={user} />
  } else if ([paths.analytics, paths.events, paths.properties, paths.servers].includes(route)) {
    page = <FuturePage path={route} />
  } else {
    page = <ProjectsPage projects={projects} incidents={incidents} refresh={refreshProjects} />
  }

  return <div className="app-shell">
    <aside className="sidebar">
      <Link to={paths.projects} className="brand"><span className="brand-mark">U</span><span>Uptime Control</span></Link>
      <nav aria-label="Основная навигация">
        <NavLink current={route} to={paths.projects}>Мониторинг</NavLink>
        <NavLink current={route} to={paths.analytics}>Аналитика</NavLink>
        <NavLink current={route} to={paths.events}>События</NavLink>
        <NavLink current={route} to={paths.properties}>Свойства</NavLink>
        <NavLink current={route} to={paths.servers}>Серверы</NavLink>
        <NavLink current={route} to={paths.settings}>Настройки</NavLink>
      </nav>
      <div className="sidebar-footer">
        <div className={`live-indicator ${liveState}`}><span />{liveState === 'live' ? 'Live' : liveState === 'polling' ? 'Polling · данные могут запаздывать' : 'Подключение'}</div>
        {lastSnapshot && <small>Снимок: {formatTime(lastSnapshot)}</small>}
        <button className="button secondary full" onClick={logout}>Выйти</button>
      </div>
    </aside>
    <main className="content">{page}</main>
  </div>
}

function NavLink({ current, to, children }: { current: string; to: string; children: ReactNode }) {
  const active = current === to || (to === paths.projects && current.startsWith('/projects/'))
  return <Link to={to} className={active ? 'nav-link active' : 'nav-link'}>{children}</Link>
}

function AuthPage({ onAuthenticated }: { onAuthenticated: (user: User) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError('')
    const values = new FormData(event.currentTarget)
    try {
      const result = await mutate<{ user: User }>(`/auth/${mode}`, 'POST', {
        email: values.get('email'), password: values.get('password'),
      })
      onAuthenticated(result.user)
      navigate(paths.projects)
    } catch (failure) {
      setError(messageOf(failure))
    } finally { setBusy(false) }
  }
  return <main className="auth-page">
    <section className="auth-card" aria-labelledby="auth-title">
      <div className="brand auth-brand"><span className="brand-mark">U</span><span>Uptime Control</span></div>
      <p className="eyebrow">SELF-HOSTED OBSERVABILITY</p>
      <h1 id="auth-title">{mode === 'login' ? 'Войдите в Dashboard' : 'Создайте владельца установки'}</h1>
      <p className="muted">Мониторинг доступности, инциденты и уведомления в одном интерфейсе.</p>
      <div className="tabs" role="tablist" aria-label="Режим авторизации">
        <button role="tab" aria-selected={mode === 'login'} onClick={() => setMode('login')}>Вход</button>
        <button role="tab" aria-selected={mode === 'register'} onClick={() => setMode('register')}>Регистрация</button>
      </div>
      <form onSubmit={submit} className="stack">
        <label>Email<input name="email" type="email" autoComplete="email" required /></label>
        <label>Пароль<input name="password" type="password" autoComplete={mode === 'login' ? 'current-password' : 'new-password'} minLength={12} maxLength={128} required /></label>
        {error && <ErrorState compact>{error}</ErrorState>}
        <button className="button primary" disabled={busy}>{busy ? 'Подождите…' : mode === 'login' ? 'Войти' : 'Создать владельца'}</button>
      </form>
    </section>
  </main>
}

function ProjectsPage({ projects, incidents, refresh }: { projects: Project[]; incidents: OpenIncident[]; refresh: () => Promise<void> }) {
  const [showCreate, setShowCreate] = useState(false)
  const [error, setError] = useState('')
  const [secret, setSecret] = useState('')
  async function createProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    const form = new FormData(event.currentTarget)
    const type = String(form.get('type')) as Monitor['type']
    const monitor: Record<string, unknown> = {
      type, name: form.get('monitor_name'), check_interval_seconds: Number(form.get('interval')),
    }
    if (type === 'http') Object.assign(monitor, { url: form.get('url'), timeout_seconds: Number(form.get('timeout')) })
    if (type === 'tcp') Object.assign(monitor, { target: form.get('target'), timeout_seconds: Number(form.get('timeout')) })
    try {
      const result = await mutate<{ project: Project }>('/projects', 'POST', {
        name: form.get('name'), description: form.get('description'), monitor,
      })
      const token = result.project.monitors[0]?.heartbeat_token
      if (token) setSecret(`${window.location.origin}${basePath}/api/v1/heartbeat/${token}`)
      event.currentTarget.reset()
      setShowCreate(false)
      await refresh()
    } catch (failure) { setError(messageOf(failure)) }
  }
  return <>
    <PageHeader eyebrow="MONITORING" title="Проекты" description="Текущее состояние всех проверок и открытых инцидентов.">
      <button className="button primary" onClick={() => setShowCreate(!showCreate)}>+ Новый проект</button>
    </PageHeader>
    {incidents.length > 0 && <section className="incident-banner" aria-live="polite">
      <strong>{incidents.length === 1 ? 'Открыт 1 инцидент' : `Открыто инцидентов: ${incidents.length}`}</strong>
      {incidents.map((item) => <span key={item.id}>{item.project_name} · {item.monitor_name} · с {formatTime(item.started_at)}</span>)}
    </section>}
    {secret && <SecretBox title="Сохраните heartbeat URL — он показывается один раз" value={secret} onClose={() => setSecret('')} />}
    {showCreate && <section className="panel form-panel">
      <h2>Новый проект</h2>
      <form onSubmit={createProject} className="form-grid">
        <label>Название проекта<input name="name" maxLength={100} required /></label>
        <label>Описание<input name="description" maxLength={2000} /></label>
        <label>Тип проверки<select name="type" defaultValue="http"><option value="http">HTTP / HTTPS</option><option value="tcp">TCP-порт</option><option value="heartbeat">Heartbeat</option></select></label>
        <label>Название проверки<input name="monitor_name" defaultValue="Основная проверка" maxLength={100} required /></label>
        <label>URL для HTTP<input name="url" type="url" placeholder="https://example.com/health" /></label>
        <label>Адрес для TCP<input name="target" placeholder="example.com:443" /></label>
        <label>Интервал, секунд<input name="interval" type="number" min={30} max={86400} defaultValue={60} required /></label>
        <label>Таймаут, секунд<input name="timeout" type="number" min={1} max={30} defaultValue={10} required /></label>
        {error && <ErrorState compact>{error}</ErrorState>}
        <div className="actions"><button className="button primary">Создать</button><button type="button" className="button secondary" onClick={() => setShowCreate(false)}>Отмена</button></div>
      </form>
    </section>}
    {!projects.length ? <EmptyState title="Проектов пока нет">Создайте проект и первую HTTP, TCP или heartbeat-проверку.</EmptyState> :
      <div className="project-grid">{projects.map((project) => <ProjectCard key={project.id} project={project} refresh={refresh} onSecret={setSecret} />)}</div>}
  </>
}

function ProjectCard({ project, refresh, onSecret }: { project: Project; refresh: () => Promise<void>; onSecret: (value: string) => void }) {
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState('')
  async function addMonitor(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const type = String(form.get('type')) as Monitor['type']
    const payload: Record<string, unknown> = { type, name: form.get('name'), check_interval_seconds: Number(form.get('interval')) }
    if (type === 'http') Object.assign(payload, { url: form.get('destination'), timeout_seconds: Number(form.get('timeout')) })
    if (type === 'tcp') Object.assign(payload, { target: form.get('destination'), timeout_seconds: Number(form.get('timeout')) })
    try {
      const result = await mutate<{ monitor: Monitor }>(`/projects/${project.id}/monitors`, 'POST', payload)
      if (result.monitor.heartbeat_token) onSecret(`${window.location.origin}${basePath}/api/v1/heartbeat/${result.monitor.heartbeat_token}`)
      setAdding(false)
      await refresh()
    } catch (failure) { setError(messageOf(failure)) }
  }
  async function toggle(monitor: Monitor) {
    await mutate(`/projects/${project.id}/monitors/${monitor.id}`, 'PATCH', { enabled: !monitor.enabled })
    await refresh()
  }
  async function removeMonitor(monitor: Monitor) {
    if (!window.confirm(`Удалить проверку «${monitor.name}»?`)) return
    await mutate(`/projects/${project.id}/monitors/${monitor.id}`, 'DELETE', undefined, { 'X-Confirm-Delete': 'true' })
    await refresh()
  }
  async function removeProject() {
    if (!window.confirm(`Удалить проект «${project.name}» и отключить его проверки?`)) return
    await mutate(`/projects/${project.id}`, 'DELETE', undefined, { 'X-Confirm-Delete': 'true' })
    await refresh()
  }
  return <article className="panel project-card">
    <header><div><p className="eyebrow">PROJECT</p><h2>{project.name}</h2><p className="muted">{project.description || 'Без описания'}</p></div><StatusBadge state={projectState(project)} /></header>
    <div className="monitor-list">
      {project.monitors.map((monitor) => <div className="monitor-row" key={monitor.id}>
        <div className="status-dot-wrap"><span className={`status-dot ${monitorState(monitor)}`} /></div>
        <div className="monitor-main"><Link to={`/projects/${project.id}/monitors/${monitor.id}`}><strong>{monitor.name}</strong></Link><small>{monitor.type.toUpperCase()} · {monitor.target || monitor.url || 'heartbeat'} · {monitor.check_interval_seconds} сек.</small></div>
        <div className="monitor-metric"><strong>{monitor.last_response_time_ms == null ? '—' : `${monitor.last_response_time_ms} мс`}</strong><small>{formatTime(monitor.last_checked_at)}</small></div>
        <div className="row-actions"><button className="icon-button" aria-label={monitor.enabled ? `Приостановить ${monitor.name}` : `Включить ${monitor.name}`} onClick={() => void toggle(monitor)}>{monitor.enabled ? 'Ⅱ' : '▶'}</button><button className="icon-button danger-text" aria-label={`Удалить ${monitor.name}`} onClick={() => void removeMonitor(monitor)}>×</button></div>
      </div>)}
    </div>
    {adding && <form className="inline-form" onSubmit={addMonitor}>
      <label>Тип<select name="type"><option value="http">HTTP</option><option value="tcp">TCP</option><option value="heartbeat">Heartbeat</option></select></label>
      <label>Название<input name="name" required maxLength={100} /></label>
      <label>URL или host:port<input name="destination" /></label>
      <label>Интервал<input name="interval" type="number" min={30} max={86400} defaultValue={60} /></label>
      <input name="timeout" type="hidden" value="10" />
      {error && <ErrorState compact>{error}</ErrorState>}
      <div className="actions"><button className="button primary small">Добавить</button><button type="button" className="button secondary small" onClick={() => setAdding(false)}>Отмена</button></div>
    </form>}
    <footer className="card-actions"><button className="text-button" onClick={() => setAdding(!adding)}>+ Добавить monitor</button><Link className="text-button" to={`/projects/${project.id}/notifications`}>Уведомления</Link><button className="text-button danger-text" onClick={() => void removeProject()}>Удалить проект</button></footer>
  </article>
}

function MonitorPage({ projectID, monitorID, projects }: { projectID: string; monitorID: string; projects: Project[] }) {
  const project = projects.find((item) => item.id === projectID)
  const monitor = project?.monitors.find((item) => item.id === monitorID)
  const [period, setPeriod] = useState<Period>('24h')
  const [timezone, setTimezone] = useState('local')
  const [summary, setSummary] = useState<Summary | null>(null)
  const [checks, setChecks] = useState<Check[]>([])
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [checkCursor, setCheckCursor] = useState<string | null>(null)
  const [incidentCursor, setIncidentCursor] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const query = useMemo(() => periodQuery(period), [period])

  const load = useCallback(async () => {
    if (!monitor) return
    setLoading(true); setError('')
    try {
      const [summaryResult, checkPage, incidentPage] = await Promise.all([
        api<{ summary: Summary }>(`/projects/${projectID}/monitors/${monitorID}/summary?${query}`),
        api<{ checks: Check[]; next_cursor: string | null }>(`/projects/${projectID}/monitors/${monitorID}/checks?limit=50&${query}`),
        api<{ incidents: Incident[]; next_cursor: string | null }>(`/projects/${projectID}/monitors/${monitorID}/incidents?limit=20&${query}`),
      ])
      setSummary(summaryResult.summary)
      setChecks(checkPage.checks)
      setIncidents(incidentPage.incidents)
      setCheckCursor(checkPage.next_cursor); setIncidentCursor(incidentPage.next_cursor)
    } catch (failure) { setError(messageOf(failure)) } finally { setLoading(false) }
  }, [monitor, projectID, monitorID, query])

  async function loadMoreChecks() {
    if (!checkCursor) return
    const page = await api<{ checks: Check[]; next_cursor: string | null }>(`/projects/${projectID}/monitors/${monitorID}/checks?limit=50&${query}&cursor=${encodeURIComponent(checkCursor)}`)
    setChecks((old) => [...old, ...page.checks]); setCheckCursor(page.next_cursor)
  }

  async function loadMoreIncidents() {
    if (!incidentCursor) return
    const page = await api<{ incidents: Incident[]; next_cursor: string | null }>(`/projects/${projectID}/monitors/${monitorID}/incidents?limit=20&${query}&cursor=${encodeURIComponent(incidentCursor)}`)
    setIncidents((old) => [...old, ...page.incidents]); setIncidentCursor(page.next_cursor)
  }

  useEffect(() => { setChecks([]); setIncidents([]); setCheckCursor(null); setIncidentCursor(null); void load() }, [period, monitorID, monitor?.id]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!project || !monitor) return <EmptyState title="Monitor не найден">Вернитесь к списку проектов и обновите данные.</EmptyState>
  return <>
    <PageHeader eyebrow={project.name} title={monitor.name} description={`${monitor.type.toUpperCase()} · ${monitor.target || monitor.url || 'heartbeat'}`}><Link className="button secondary" to={paths.projects}>← Проекты</Link></PageHeader>
    <div className="toolbar"><label>Период<select value={period} onChange={(e) => setPeriod(e.target.value as Period)}><option value="24h">24 часа</option><option value="7d">7 дней</option><option value="30d">30 дней</option></select></label><label>Часовой пояс<select value={timezone} onChange={(e) => setTimezone(e.target.value)}><option value="local">Локальный</option><option value="UTC">UTC</option></select></label></div>
    {error && <ErrorState>{error}<button className="button secondary small" onClick={() => void load()}>Повторить</button></ErrorState>}
    {loading && !summary ? <LoadingState /> : summary && <>
      <section className="metric-grid"><Metric label="Статус" value={statusLabel(summary.status)} /><Metric label="Uptime" value={formatPercent(summary.uptime_percent)} /><Metric label="Покрытие" value={formatPercent(summary.coverage_percent)} /><Metric label="Среднее" value={summary.average_response_time_ms == null ? '—' : `${Math.round(summary.average_response_time_ms)} мс`} /><Metric label="Пик" value={summary.peak_response_time_ms == null ? '—' : `${summary.peak_response_time_ms} мс`} /></section>
      <section className="panel"><h2>Время ответа</h2><ResponseChart checks={checks} /></section>
      <section className="panel"><h2>Проверки</h2><DataTable headers={['Время', 'Результат', 'Код', 'Ответ']} rows={checks.map((item) => [formatTime(item.checked_at, timezone), item.available ? 'Доступен' : item.error || 'Недоступен', item.status_code ?? '—', item.response_time_ms == null ? '—' : `${item.response_time_ms} мс`])} empty="За период нет проверок" />{checkCursor && <button className="button secondary small" onClick={() => void loadMoreChecks()}>Показать ещё</button>}</section>
      <section className="panel"><h2>Инциденты</h2><DataTable headers={['Начало', 'Завершение', 'Длительность', 'Причина']} rows={incidents.map((item) => [formatTime(item.started_at, timezone), item.resolved_at ? formatTime(item.resolved_at, timezone) : 'Открыт', formatDuration(item.duration_seconds), item.cause || '—'])} empty="За период нет инцидентов" />{incidentCursor && <button className="button secondary small" onClick={() => void loadMoreIncidents()}>Показать ещё</button>}</section>
    </>}
  </>
}

function NotificationsPage({ projectID, projects }: { projectID: string; projects: Project[] }) {
  const project = projects.find((item) => item.id === projectID)
  const [hooks, setHooks] = useState<Webhook[]>([])
  const [notifications, setNotifications] = useState<Notification[]>([])
  const [secret, setSecret] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const [webhooks, page] = await Promise.all([
        api<{ webhooks: Webhook[] }>(`/projects/${projectID}/webhooks`),
        api<{ notifications: Notification[] }>(`/projects/${projectID}/notifications?limit=50`),
      ])
      setHooks(webhooks.webhooks); setNotifications(page.notifications)
    } catch (failure) { setError(messageOf(failure)) } finally { setLoading(false) }
  }, [projectID])
  useEffect(() => { void load() }, [load])
  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = new FormData(event.currentTarget)
    try {
      const result = await mutate<{ webhook: Webhook }>(`/projects/${projectID}/webhooks`, 'POST', { url: form.get('url'), events: form.get('events') })
      setSecret(result.webhook.secret || '')
      event.currentTarget.reset(); await load()
    } catch (failure) { setError(messageOf(failure)) }
  }
  async function toggle(hook: Webhook) { await mutate(`/projects/${projectID}/webhooks/${hook.id}`, 'PATCH', { enabled: !hook.enabled }); await load() }
  async function remove(hook: Webhook) { if (confirm('Удалить webhook?')) { await mutate(`/projects/${projectID}/webhooks/${hook.id}`, 'DELETE', undefined, { 'X-Confirm-Delete': 'true' }); await load() } }
  async function retry(item: Notification) { if (confirm('Повторить доставку?')) { await mutate(`/projects/${projectID}/notifications/${item.id}/retry`, 'POST', undefined, { 'X-Confirm-Retry': 'true' }); await load() } }
  if (!project) return <EmptyState title="Проект не найден">Обновите список проектов.</EmptyState>
  return <>
    <PageHeader eyebrow={project.name} title="Уведомления" description="Webhook-подписки и журнал устойчивой доставки."><Link className="button secondary" to={paths.projects}>← Проекты</Link></PageHeader>
    {secret && <SecretBox title="Сохраните HMAC-секрет — он показывается один раз" value={secret} onClose={() => setSecret('')} />}
    {error && <ErrorState>{error}</ErrorState>}
    <section className="panel"><h2>Новый webhook</h2><form className="inline-form" onSubmit={create}><label>Публичный URL<input name="url" type="url" required placeholder="https://hooks.example.com/uptime" /></label><label>События<select name="events" defaultValue="down,recovered"><option value="down,recovered">Падение и восстановление</option><option value="down">Только падение</option><option value="recovered">Только восстановление</option></select></label><button className="button primary">Добавить</button></form></section>
    <section className="panel"><div className="section-heading"><h2>Каналы</h2><button className="button secondary small" onClick={() => void mutate(`/projects/${projectID}/notifications/test`, 'POST').then(load)}>Отправить тест</button></div>{loading ? <LoadingState /> : !hooks.length ? <EmptyState title="Webhooks не настроены">Telegram настраивается переменными окружения сервера.</EmptyState> : hooks.map((hook) => <div className="list-row" key={hook.id}><div><strong>{hook.url}</strong><small>{hook.events} · {hook.enabled ? 'включён' : 'выключен'}</small></div><div className="actions"><button className="button secondary small" onClick={() => void toggle(hook)}>{hook.enabled ? 'Выключить' : 'Включить'}</button><button className="button danger small" onClick={() => void remove(hook)}>Удалить</button></div></div>)}</section>
    <section className="panel"><h2>Журнал доставки</h2><DataTable headers={['Время', 'Событие', 'Monitor', 'Статус', 'Попытки', 'Действие']} rows={notifications.map((item) => [formatTime(item.occurred_at), item.kind, item.monitor_name, item.last_error || item.status, String(item.attempts), item.status === 'failed' ? <button className="text-button" onClick={() => void retry(item)}>Повторить</button> : '—'])} empty="Событий доставки пока нет" /></section>
  </>
}

function SettingsPage({ user }: { user: User }) {
  return <><PageHeader eyebrow="SETTINGS" title="Настройки" description="Параметры владельца и текущей установки." /><section className="panel settings-list"><div><span>Email владельца</span><strong>{user.email}</strong></div><div><span>Путь установки</span><strong>{basePath || '/'}</strong></div><div><span>Сессия</span><strong>HttpOnly cookie · 7 дней</strong></div><div><span>Обновления</span><strong>SSE с polling fallback</strong></div></section></>
}

function FuturePage({ path }: { path: string }) {
  const content: Record<string, [string, string]> = {
    [paths.analytics]: ['Аналитика', 'Посещаемость, источники и производительность появятся после подключения Analytics API.'],
    [paths.events]: ['События', 'Custom events и их временная шкала будут подключены к реальным данным на следующих этапах.'],
    [paths.properties]: ['Свойства', 'Управление схемой custom properties подготовлено как отдельный раздел.'],
    [paths.servers]: ['Серверы', 'Региональные узлы и Server Agent будут отображаться здесь после регистрации.'],
  }
  const [title, description] = content[path]
  return <><PageHeader eyebrow="COMING NEXT" title={title} description={description} /><EmptyState title="Раздел подготовлен">Навигация и доступный пустой экран готовы; данные появятся вместе с соответствующим backend API.</EmptyState></>
}

function PageHeader({ eyebrow, title, description, children }: { eyebrow: string; title: string; description: string; children?: ReactNode }) {
  return <header className="page-header"><div><p className="eyebrow">{eyebrow}</p><h1>{title}</h1><p className="muted">{description}</p></div><div className="actions">{children}</div></header>
}
function FullPageState({ children }: { children: ReactNode }) { return <main className="center-page"><div className="spinner" /><p>{children}</p></main> }
function LoadingState() { return <div className="state-row" aria-live="polite"><div className="spinner" />Загрузка…</div> }
function ErrorState({ children, compact = false }: { children: ReactNode; compact?: boolean }) { return <div className={compact ? 'error-state compact' : 'error-state'} role="alert">{children}</div> }
function EmptyState({ title, children }: { title: string; children: ReactNode }) { return <div className="empty-state"><span>○</span><h2>{title}</h2><p>{children}</p></div> }
function Metric({ label, value }: { label: string; value: string }) { return <div className="metric"><span>{label}</span><strong>{value}</strong></div> }
function SecretBox({ title, value, onClose }: { title: string; value: string; onClose: () => void }) { return <div className="secret-box"><div><strong>{title}</strong><code>{value}</code></div><button className="icon-button" aria-label="Закрыть" onClick={onClose}>×</button></div> }
function StatusBadge({ state }: { state: string }) { return <span className={`status-badge ${state}`}>{state === 'up' ? 'Работает' : state === 'down' ? 'Сбой' : 'Ожидание'}</span> }

function DataTable({ headers, rows, empty }: { headers: string[]; rows: ReactNode[][]; empty: string }) {
  if (!rows.length) return <p className="muted">{empty}</p>
  return <div className="table-scroll"><table><thead><tr>{headers.map((item) => <th key={item}>{item}</th>)}</tr></thead><tbody>{rows.map((row, index) => <tr key={index}>{row.map((cell, cellIndex) => <td key={cellIndex}>{cell}</td>)}</tr>)}</tbody></table></div>
}

function ResponseChart({ checks }: { checks: Check[] }) {
  const points = checks.filter((item) => item.response_time_ms != null).slice().reverse()
  if (!points.length) return <p className="muted">Нет данных response time.</p>
  const width = 900, height = 220, pad = 22
  const max = Math.max(...points.map((item) => item.response_time_ms || 0), 1)
  const values = points.map((item, index) => ({ x: pad + index / Math.max(points.length - 1, 1) * (width - pad * 2), y: height - pad - (item.response_time_ms || 0) / max * (height - pad * 2), item }))
  return <div className="chart-wrap"><svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="График времени ответа"><polyline points={values.map((point) => `${point.x},${point.y}`).join(' ')} fill="none" stroke="currentColor" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" />{values.map((point) => <circle key={point.item.id} cx={point.x} cy={point.y} r="5"><title>{point.item.response_time_ms} мс · {formatTime(point.item.checked_at)}</title></circle>)}</svg><small>Максимум на графике: {max} мс</small></div>
}

function monitorState(monitor: Monitor) {
  if (!monitor.enabled || !monitor.last_checked_at) return 'pending'
  if (Date.now() - new Date(monitor.last_checked_at).getTime() > Math.max(300, 2 * monitor.check_interval_seconds) * 1000) return 'pending'
  return monitor.last_available ? 'up' : 'down'
}
function projectState(project: Project) { return project.monitors.some((item) => monitorState(item) === 'down') ? 'down' : project.monitors.some((item) => monitorState(item) === 'up') ? 'up' : 'pending' }
function statusLabel(status: string) { return ({ up: 'Работает', down: 'Недоступен', paused: 'Приостановлен', stale: 'Данные устарели', unknown: 'Ожидает данных' } as Record<string, string>)[status] || status }
function formatPercent(value: number | null) { return value == null ? '—' : `${value.toFixed(2)}%` }
function formatDuration(seconds: number) { const hours = Math.floor(seconds / 3600); const minutes = Math.floor((seconds % 3600) / 60); return hours ? `${hours} ч ${minutes} мин` : `${minutes} мин` }
function formatTime(value: string | null, timezone = 'local') { return value ? new Intl.DateTimeFormat('ru-RU', { dateStyle: 'short', timeStyle: 'medium', timeZone: timezone === 'UTC' ? 'UTC' : undefined }).format(new Date(value)) : 'ещё нет данных' }
function periodQuery(period: Period) { const to = new Date(); const duration = period === '24h' ? 86_400_000 : period === '7d' ? 7 * 86_400_000 : 30 * 86_400_000; return `from=${encodeURIComponent(new Date(to.getTime() - duration).toISOString())}&to=${encodeURIComponent(to.toISOString())}` }
function messageOf(error: unknown) { return error instanceof Error ? error.message : 'Не удалось выполнить запрос' }
