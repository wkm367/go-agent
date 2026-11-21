import DOMPurify from 'dompurify'
import * as echarts from 'echarts'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import css from 'highlight.js/lib/languages/css'
import go from 'highlight.js/lib/languages/go'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import markdown from 'highlight.js/lib/languages/markdown'
import python from 'highlight.js/lib/languages/python'
import sql from 'highlight.js/lib/languages/sql'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import 'highlight.js/styles/github-dark.css'
import {
  Activity,
  ArrowUp,
  BarChart3,
  Bot,
  Check,
  createIcons,
  Database,
  FileText,
  LockKeyhole,
  LogOut,
  Menu,
  MessageSquare,
  Pencil,
  Plus,
  Sparkles,
  Square,
  SquarePen,
  ShieldCheck,
  Trash2,
  Upload,
  UserRound,
  X,
} from 'lucide'
import { marked } from 'marked'
import './styles.css'

hljs.registerLanguage('bash', bash)
hljs.registerLanguage('css', css)
hljs.registerLanguage('go', go)
hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('json', json)
hljs.registerLanguage('markdown', markdown)
hljs.registerLanguage('python', python)
hljs.registerLanguage('sql', sql)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('xml', xml)

const API_BASE = (import.meta.env.VITE_API_BASE_URL || '/api').replace(/\/$/, '')
const TOKEN_STORAGE_KEY = 'go-agentery.token'
const USER_STORAGE_KEY = 'go-agentery.user'
const SESSION_STORAGE_KEY = 'go-agentery.active-session-id'
const ICONS = {
  Activity,
  ArrowUp,
  BarChart3,
  Bot,
  Check,
  Database,
  FileText,
  LockKeyhole,
  LogOut,
  Menu,
  MessageSquare,
  Pencil,
  Plus,
  Sparkles,
  Square,
  SquarePen,
  ShieldCheck,
  Trash2,
  Upload,
  UserRound,
  X,
}

const elements = {
  appShell: document.querySelector('#app-shell'),
  authError: document.querySelector('#auth-error'),
  authForm: document.querySelector('#auth-form'),
  authModeLabel: document.querySelector('#auth-mode-label'),
  authModeSwitch: document.querySelector('#auth-mode-switch'),
  authPassword: document.querySelector('#auth-password'),
  authPasswordConfirm: document.querySelector('#auth-password-confirm'),
  authSubmit: document.querySelector('#auth-submit'),
  authSubmitLabel: document.querySelector('#auth-submit span'),
  authSubtitle: document.querySelector('#auth-subtitle'),
  authSwitchCopy: document.querySelector('#auth-switch-copy'),
  authTitle: document.querySelector('#auth-title'),
  authUsername: document.querySelector('#auth-username'),
  authView: document.querySelector('#auth-view'),
  chatScroll: document.querySelector('#chat-scroll'),
  closeHistory: document.querySelector('#close-history'),
  closeTrace: document.querySelector('#close-trace'),
  composer: document.querySelector('#composer'),
  connectionStatus: document.querySelector('#connection-status'),
  confirmPasswordField: document.querySelector('#confirm-password-field'),
  confirmPasswordStatus: document.querySelector('#confirm-password-status'),
  currentUsername: document.querySelector('#current-username'),
  currentSessionTitle: document.querySelector('#current-session-title'),
  datasetError: document.querySelector('#dataset-error'),
  datasetFile: document.querySelector('#dataset-file'),
  datasetFileName: document.querySelector('#dataset-file-name'),
  datasetForm: document.querySelector('#dataset-form'),
  datasetModal: document.querySelector('#dataset-modal'),
  datasetModalBackdrop: document.querySelector('#dataset-modal-backdrop'),
  datasetName: document.querySelector('#dataset-name'),
  datasetActions: document.querySelector('#dataset-actions'),
  datasetSelect: document.querySelector('#dataset-select'),
  datasetSubmit: document.querySelector('#dataset-submit'),
  closeDatasetModal: document.querySelector('#close-dataset-modal'),
  emptyState: document.querySelector('#empty-state'),
  historySidebar: document.querySelector('#history-sidebar'),
  historyToggle: document.querySelector('#history-toggle'),
  input: document.querySelector('#message-input'),
  logout: document.querySelector('#logout-button'),
  messages: document.querySelector('#messages'),
  newSession: document.querySelector('#new-session'),
  newSessionTop: document.querySelector('#new-session-top'),
  passwordHint: document.querySelector('#password-hint'),
  passwordStrength: document.querySelector('#password-strength'),
  passwordStrengthLabel: document.querySelector('#password-strength-label'),
  renameDatasetBackdrop: document.querySelector('#rename-dataset-backdrop'),
  renameDatasetError: document.querySelector('#rename-dataset-error'),
  renameDatasetForm: document.querySelector('#rename-dataset-form'),
  renameDatasetModal: document.querySelector('#rename-dataset-modal'),
  renameDatasetName: document.querySelector('#rename-dataset-name'),
  renameDatasetSubmit: document.querySelector('#rename-dataset-submit'),
  deleteDataset: document.querySelector('#delete-dataset'),
  renameDataset: document.querySelector('#rename-dataset'),
  closeRenameDataset: document.querySelector('#close-rename-dataset'),
  send: document.querySelector('#send-message'),
  sessionCount: document.querySelector('#session-count'),
  sessionList: document.querySelector('#session-list'),
  sidebarOverlay: document.querySelector('#sidebar-overlay'),
  stop: document.querySelector('#stop-generation'),
  traceList: document.querySelector('#trace-list'),
  traceOverlay: document.querySelector('#trace-overlay'),
  tracePanel: document.querySelector('#trace-panel'),
  traceSummary: document.querySelector('#trace-summary'),
  traceToggle: document.querySelector('#trace-toggle'),
  uploadDataset: document.querySelector('#upload-dataset'),
}

class ApiError extends Error {
  constructor(status, message) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

function isUnauthorized(error) {
  return error instanceof ApiError && error.status === 401
}

const state = {
  activeRunId: '',
  authMode: 'login',
  controller: null,
  generating: false,
  datasets: [],
  lastRun: null,
  runEvents: [],
  sessionId: localStorage.getItem(SESSION_STORAGE_KEY) || '',
  sessions: [],
  token: localStorage.getItem(TOKEN_STORAGE_KEY) || '',
  user: readStoredUser(),
}

marked.setOptions({
  breaks: true,
  gfm: true,
})

function readStoredUser() {
  try {
    return JSON.parse(localStorage.getItem(USER_STORAGE_KEY) || 'null')
  } catch {
    return null
  }
}

function createLucideIcons(root = document) {
  createIcons({
    icons: ICONS,
    attrs: {
      'aria-hidden': 'true',
      'stroke-width': 2,
    },
    nameAttr: 'data-lucide',
    root,
  })
}

function setConnectionStatus(status, label) {
  elements.connectionStatus.className = `connection-status is-${status}`
  elements.connectionStatus.innerHTML = `<span class="status-dot"></span>${label}`
}

function setAuthError(message = '') {
  elements.authError.hidden = !message
  elements.authError.textContent = message
}

function passwordStrength(password) {
  if (!password) {
    return { level: 'empty', label: '请输入密码', score: 0 }
  }
  let score = 0
  if (password.length >= 8) score++
  if (/[a-zA-Z]/.test(password) && /\d/.test(password)) score++
  if (/[^a-zA-Z0-9]/.test(password) || password.length >= 12) score++

  if (score <= 1) {
    return { level: 'weak', label: '密码强度较弱', score }
  }
  if (score === 2) {
    return { level: 'medium', label: '密码强度适中', score }
  }
  return { level: 'strong', label: '密码强度良好', score }
}

function updatePasswordStrength() {
  const strength = passwordStrength(elements.authPassword.value)
  elements.passwordStrength.dataset.strength = strength.level
  elements.passwordStrengthLabel.textContent = strength.label
}

function updatePasswordConfirmation() {
  const password = elements.authPassword.value
  const confirmation = elements.authPasswordConfirm.value
  elements.confirmPasswordStatus.className = ''
  if (!confirmation) {
    elements.confirmPasswordStatus.textContent = '再次输入密码'
    return
  }
  if (password === confirmation) {
    elements.confirmPasswordStatus.textContent = '两次密码一致'
    elements.confirmPasswordStatus.className = 'is-valid'
    return
  }
  elements.confirmPasswordStatus.textContent = '两次密码不一致'
  elements.confirmPasswordStatus.className = 'is-invalid'
}

function setAuthMode(mode) {
  state.authMode = mode
  const registering = mode === 'register'

  document.querySelectorAll('[data-auth-mode]').forEach((button) => {
    const active = button.dataset.authMode === mode
    button.classList.toggle('is-active', active)
    button.setAttribute('aria-selected', String(active))
  })

  elements.authModeLabel.textContent = registering ? 'CREATE ACCOUNT' : 'ACCOUNT ACCESS'
  elements.authTitle.textContent = registering ? '创建你的账号' : '欢迎回来'
  elements.authSubtitle.textContent = registering
    ? '注册后即可开始第一次经营分析'
    : '登录后继续你的经营分析任务'
  elements.authSwitchCopy.textContent = registering ? '已经有账号？' : '还没有账号？'
  elements.authModeSwitch.textContent = registering ? '返回登录' : '立即注册'
  elements.authModeSwitch.dataset.authMode = registering ? 'login' : 'register'
  elements.confirmPasswordField.hidden = !registering
  elements.passwordStrength.hidden = !registering
  elements.passwordHint.textContent = registering ? '建议同时包含字母和数字' : '至少 8 个字符'
  elements.authPassword.autocomplete = registering ? 'new-password' : 'current-password'
  elements.authPasswordConfirm.required = registering
  elements.authSubmitLabel.textContent = registering ? '注册并登录' : '登录'
  updatePasswordStrength()
  updatePasswordConfirmation()
  setAuthError()
}

function showAuthView() {
  elements.authView.hidden = false
  elements.appShell.hidden = true
  elements.authPassword.value = ''
  elements.authPasswordConfirm.value = ''
  updatePasswordStrength()
  updatePasswordConfirmation()
  setAuthError()
  elements.authUsername.focus()
}

function showAppView() {
  elements.authView.hidden = true
  elements.appShell.hidden = false
  elements.currentUsername.textContent = state.user?.username || '用户'
}

function persistAuthentication(payload) {
  state.token = payload.token
  state.user = payload.user
  localStorage.setItem(TOKEN_STORAGE_KEY, state.token)
  localStorage.setItem(USER_STORAGE_KEY, JSON.stringify(state.user))
}

function clearAuthentication() {
  state.token = ''
  state.user = null
  state.sessionId = ''
  state.sessions = []
  state.datasets = []
  localStorage.removeItem(TOKEN_STORAGE_KEY)
  localStorage.removeItem(USER_STORAGE_KEY)
  localStorage.removeItem(SESSION_STORAGE_KEY)
  resetTrace()
}

function logout() {
  stopGeneration()
  clearAuthentication()
  elements.messages.replaceChildren()
  elements.sessionList.replaceChildren()
  elements.sessionCount.textContent = '0'
  renderDatasetOptions()
  setAuthMode('login')
  showAuthView()
}

function handleUnauthorized() {
  stopGeneration()
  clearAuthentication()
  elements.messages.replaceChildren()
  elements.sessionList.replaceChildren()
  elements.sessionCount.textContent = '0'
  state.datasets = []
  renderDatasetOptions()
  setAuthMode('login')
  showAuthView()
}

async function submitAuthentication() {
  const username = elements.authUsername.value.trim()
  const password = elements.authPassword.value

  if (!username) {
    setAuthError('请输入用户名')
    return
  }
  if (password.length < 8) {
    setAuthError('密码至少需要 8 个字符')
    return
  }
  if (state.authMode === 'register' && password !== elements.authPasswordConfirm.value) {
    setAuthError('两次输入的密码不一致')
    return
  }

  elements.authSubmit.disabled = true
  elements.authSubmitLabel.textContent = state.authMode === 'register' ? '注册中...' : '登录中...'
  setAuthError()

  try {
    const response = await fetch(`${API_BASE}/auth/${state.authMode}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ username, password }),
    })
    const payload = await response.json()
    if (!response.ok) {
      throw new ApiError(response.status, payload.error || '认证失败')
    }

    persistAuthentication(payload)
    showAppView()
    await initializeChat()
  } catch (error) {
    setAuthError(error.message || '认证失败，请稍后重试')
  } finally {
    elements.authSubmit.disabled = false
    elements.authSubmitLabel.textContent = state.authMode === 'register' ? '注册并登录' : '登录'
  }
}

function setGenerating(generating) {
  state.generating = generating
  elements.stop.hidden = !generating
  elements.input.disabled = generating
  elements.send.disabled = generating || !elements.input.value.trim()
  elements.newSession.disabled = generating
  elements.newSessionTop.disabled = generating
  elements.datasetSelect.disabled = generating
  elements.uploadDataset.disabled = generating
  updateDatasetActions()

  if (!generating) {
    elements.input.focus()
  }
}

function updateSendButton() {
  elements.send.disabled = state.generating || !elements.input.value.trim()
}

function resizeInput() {
  elements.input.style.height = 'auto'
  elements.input.style.height = `${Math.min(elements.input.scrollHeight, 160)}px`
}

function scrollToBottom(force = false) {
  const distance =
    elements.chatScroll.scrollHeight -
    elements.chatScroll.scrollTop -
    elements.chatScroll.clientHeight

  if (force || distance < 140) {
    elements.chatScroll.scrollTop = elements.chatScroll.scrollHeight
  }
}

function setSidebarOpen(open) {
  elements.historySidebar.classList.toggle('is-open', open)
  elements.sidebarOverlay.hidden = !open
  elements.historyToggle.setAttribute('aria-expanded', String(open))
}

function setCurrentTitle(title) {
  elements.currentSessionTitle.textContent = title || '新会话'
}

function showEmptyState(show) {
  elements.emptyState.hidden = !show
}

function setTraceOpen(open) {
  elements.tracePanel.classList.toggle('is-open', open)
  elements.traceOverlay.hidden = !open
}

function resetTrace(message = '发送分析问题后，这里会展示计划、SQL、工具结果和产物。') {
  state.activeRunId = ''
  state.lastRun = null
  state.runEvents = []
  elements.traceList.replaceChildren()
  elements.traceSummary.textContent = message
}

function eventKey(event) {
  const data = event.data || {}
  const identity = data.id || data.step_id || data.run_id || ''
  return `${event.type}:${event.sequence || 0}:${identity}:${event.timestamp || ''}`
}

function appendTraceEvent(event) {
  const key = eventKey(event)
  if (state.runEvents.some((item) => eventKey(item) === key)) {
    return
  }
  state.runEvents.push(event)
  renderTrace()
}

function formatEventTime(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return date.toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function parseJSON(value, fallback = null) {
  try {
    return JSON.parse(value)
  } catch {
    return fallback
  }
}

function createTraceEntry(type) {
  const entry = document.createElement('article')
  entry.className = `trace-entry trace-entry--${type}`
  return entry
}

function appendTraceHeading(entry, title, detail = '', timestamp = '') {
  const header = document.createElement('div')
  header.className = 'trace-entry-header'

  const strong = document.createElement('strong')
  strong.textContent = title
  header.append(strong)

  if (detail) {
    const badge = document.createElement('span')
    badge.textContent = detail
    header.append(badge)
  }
  if (timestamp) {
    const time = document.createElement('time')
    time.textContent = formatEventTime(timestamp)
    header.append(time)
  }
  entry.append(header)
}

function appendCodeBlock(entry, code) {
  const pre = document.createElement('pre')
  pre.textContent = code
  entry.append(pre)
}

function appendResultTable(entry, result) {
  if (!result || !Array.isArray(result.columns) || !Array.isArray(result.rows)) {
    return
  }
  const table = document.createElement('table')
  const head = document.createElement('thead')
  const headRow = document.createElement('tr')
  for (const column of result.columns) {
    const cell = document.createElement('th')
    cell.textContent = column
    headRow.append(cell)
  }
  head.append(headRow)
  table.append(head)

  const body = document.createElement('tbody')
  for (const row of result.rows.slice(0, 8)) {
    const tableRow = document.createElement('tr')
    for (const column of result.columns) {
      const cell = document.createElement('td')
      cell.textContent = row[column] ?? '—'
      tableRow.append(cell)
    }
    body.append(tableRow)
  }
  table.append(body)
  entry.append(table)
}

function renderTraceArtifact(artifact) {
  const entry = createTraceEntry('artifact')
  appendTraceHeading(
    entry,
    artifact.title || '分析产物',
    artifact.type || 'artifact',
    artifact.created_at,
  )

  if (artifact.type === 'chart') {
    const chart = document.createElement('div')
    chart.className = 'trace-chart'
    chart.dataset.chartPayload = artifact.payload
    entry.append(chart)
  } else if (artifact.type === 'table') {
    appendResultTable(entry, parseJSON(artifact.payload))
  } else if (artifact.type === 'report') {
    const summary = document.createElement('p')
    summary.textContent = '最终经营报告已生成，并写入当前会话。'
    entry.append(summary)
  }
  elements.traceList.append(entry)
}

function renderTrace() {
  elements.traceList.replaceChildren()
  const completedTools = new Set(
    state.runEvents
      .filter((event) => event.type === 'tool.completed' && event.data?.id)
      .map((event) => event.data.id),
  )

  for (const event of state.runEvents) {
    const data = event.data || {}
    if (event.type === 'run.started') {
      state.lastRun = data
      const entry = createTraceEntry('run')
      appendTraceHeading(entry, '分析任务已启动', '只读分析', event.timestamp)
      elements.traceList.append(entry)
    } else if (event.type === 'plan.created') {
      const entry = createTraceEntry('plan')
      appendTraceHeading(entry, '分析计划', `${data.plan?.length || 0} 步`, event.timestamp)
      const list = document.createElement('ol')
      for (const step of data.plan || []) {
        const item = document.createElement('li')
        item.textContent = `${step.title}：${step.description}`
        list.append(item)
      }
      entry.append(list)
      elements.traceList.append(entry)
    } else if (event.type === 'step.started' || event.type === 'step.completed') {
      if (!data.id) {
        continue
      }
      const entry = createTraceEntry('step')
      appendTraceHeading(
        entry,
        data.title || data.type || '执行步骤',
        event.type === 'step.completed' ? '完成' : '执行中',
        event.timestamp,
      )
      elements.traceList.append(entry)
    } else if (event.type === 'tool.started' && !completedTools.has(data.id)) {
      const entry = createTraceEntry('tool')
      appendTraceHeading(entry, data.name || '工具调用', '执行中', event.timestamp)
      appendCodeBlock(entry, data.arguments || '{}')
      elements.traceList.append(entry)
    } else if (event.type === 'tool.completed') {
      const entry = createTraceEntry('tool')
      appendTraceHeading(
        entry,
        data.name || '工具调用',
        data.status === 'succeeded' ? `${data.duration_ms || 0} ms` : data.status,
        event.timestamp,
      )
      if (data.name === 'run_readonly_sql') {
        const argumentsValue = parseJSON(data.arguments, {})
        appendCodeBlock(entry, argumentsValue.sql || data.arguments || '')
      }
      if (data.name === 'run_readonly_sql' && data.result) {
        appendResultTable(entry, parseJSON(data.result))
      }
      if (data.error) {
        const error = document.createElement('p')
        error.className = 'trace-error'
        error.textContent = data.error
        entry.append(error)
      }
      elements.traceList.append(entry)
    } else if (event.type === 'artifact.created') {
      renderTraceArtifact(data)
    } else if (event.type === 'run.failed') {
      const entry = createTraceEntry('failed')
      appendTraceHeading(entry, '分析任务失败', '失败', event.timestamp)
      const error = document.createElement('p')
      error.className = 'trace-error'
      error.textContent = data.error || '未知错误'
      entry.append(error)
      elements.traceList.append(entry)
    } else if (event.type === 'run.cancelled') {
      const entry = createTraceEntry('cancelled')
      appendTraceHeading(entry, '分析任务已取消', '已停止', event.timestamp)
      elements.traceList.append(entry)
    }
  }

  const run = state.lastRun
  if (run?.status) {
    elements.traceSummary.textContent = `状态：${run.status} · Token：${run.total_tokens || 0}`
  } else if (state.runEvents.length > 0) {
    elements.traceSummary.textContent = `${state.runEvents.length} 个运行事件`
  }

  requestAnimationFrame(() => {
    elements.traceList.querySelectorAll('.trace-chart').forEach((container) => {
      const payload = parseJSON(container.dataset.chartPayload, null)
      if (!payload) {
        return
      }
      const instance = echarts.getInstanceByDom(container) || echarts.init(container)
      instance.setOption({
        animationDuration: 250,
        color: ['#0f7a5a', '#d78a2d', '#3f6f91', '#9b5c73', '#6e8d3f'],
        title: {
          text: payload.title,
          textStyle: { fontSize: 13, color: '#273029' },
        },
        tooltip: { trigger: payload.chart_type === 'pie' ? 'item' : 'axis' },
        legend: { bottom: 0, textStyle: { color: '#657069', fontSize: 10 } },
        grid: { left: 42, right: 16, top: 44, bottom: 38 },
        xAxis: payload.chart_type === 'pie'
          ? undefined
          : { type: 'category', data: payload.x_values, axisLabel: { color: '#6b746e' } },
        yAxis: payload.chart_type === 'pie'
          ? undefined
          : { type: 'value', axisLabel: { color: '#6b746e' }, splitLine: { lineStyle: { color: '#e7ebe8' } } },
        series: payload.chart_type === 'pie'
          ? [{ type: 'pie', radius: ['38%', '68%'], data: payload.x_values.map((name, index) => ({
              name,
              value: payload.series[0]?.data?.[index],
            })) }]
          : payload.series.map((series) => ({
              name: series.name,
              type: payload.chart_type,
              smooth: payload.chart_type === 'line',
              data: series.data,
            })),
      })
    })
  })
}

function renderRunDetail(detail) {
  resetTrace()
  const run = detail?.run
  if (!run) {
    return
  }
  state.activeRunId = run.id
  appendTraceEvent({
    run_id: run.id,
    type: 'run.started',
    timestamp: run.created_at,
    data: run,
  })
  if (run.plan?.length) {
    appendTraceEvent({
      run_id: run.id,
      type: 'plan.created',
      timestamp: run.created_at,
      data: { plan: run.plan },
    })
  }
  for (const toolCall of detail.tool_calls || []) {
    appendTraceEvent({
      run_id: run.id,
      type: 'tool.completed',
      timestamp: toolCall.created_at,
      data: toolCall,
    })
  }
  for (const artifact of detail.artifacts || []) {
    appendTraceEvent({
      run_id: run.id,
      type: 'artifact.created',
      timestamp: artifact.created_at,
      data: artifact,
    })
  }
  if (run.status === 'succeeded') {
    appendTraceEvent({
      run_id: run.id,
      type: 'run.completed',
      timestamp: run.finished_at || run.updated_at,
      data: run,
    })
  } else if (run.status === 'failed') {
    appendTraceEvent({
      run_id: run.id,
      type: 'run.failed',
      timestamp: run.finished_at || run.updated_at,
      data: run,
    })
  } else if (run.status === 'cancelled') {
    appendTraceEvent({
      run_id: run.id,
      type: 'run.cancelled',
      timestamp: run.finished_at || run.updated_at,
      data: run,
    })
  }
}

function renderMarkdown(text) {
  const html = marked.parse(text)
  return DOMPurify.sanitize(html)
}

function highlightCode(container) {
  container.querySelectorAll('pre code').forEach((block) => {
    hljs.highlightElement(block)
  })
}

function addStatusBadge(container, status) {
  if (status !== 'partial') {
    return
  }
  const badge = document.createElement('span')
  badge.className = 'message-status'
  badge.textContent = '已停止'
  container.append(badge)
}

function createMessage(role, content = '', options = {}) {
  const { loading = false, status = 'complete' } = options
  showEmptyState(false)

  const article = document.createElement('article')
  article.className = `message message--${role}`

  const avatar = document.createElement('div')
  avatar.className = 'message-avatar'
  avatar.innerHTML = `<i data-lucide="${role === 'user' ? 'user-round' : 'bot'}"></i>`

  const body = document.createElement('div')
  body.className = 'message-body'

  if (loading) {
    body.innerHTML = '<span class="typing-dots"><span></span><span></span><span></span></span>'
  } else if (role === 'assistant') {
    body.innerHTML = renderMarkdown(content)
    highlightCode(body)
    addStatusBadge(body, status)
  } else {
    body.textContent = content
  }

  if (role === 'user') {
    article.append(body, avatar)
  } else {
    article.append(avatar, body)
  }

  elements.messages.append(article)
  createLucideIcons(article)
  scrollToBottom(true)

  return { article, body }
}

async function apiRequest(path, options = {}) {
  const headers = new Headers(options.headers || {})
  if (state.token) {
    headers.set('Authorization', `Bearer ${state.token}`)
  }

  const response = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers,
  })
  if (response.ok) {
    return response
  }

  if (response.status === 401) {
    handleUnauthorized()
  }

  let message = `请求失败（${response.status}）`
  try {
    const payload = await response.json()
    message = payload.error || payload.message || message
  } catch {
    // Keep the HTTP fallback message.
  }
  throw new ApiError(response.status, message)
}

function formatSessionTime(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }

  const now = new Date()
  if (date.toDateString() === now.toDateString()) {
    return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }
  return date.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })
}

function renderSessionList() {
  elements.sessionList.replaceChildren()
  elements.sessionCount.textContent = String(state.sessions.length)

  if (state.sessions.length === 0) {
    const empty = document.createElement('p')
    empty.className = 'session-list-empty'
    empty.textContent = '发送第一条消息后会出现在这里'
    elements.sessionList.append(empty)
    return
  }

  for (const session of state.sessions) {
    const row = document.createElement('div')
    row.className = 'session-row'
    if (session.id === state.sessionId) {
      row.classList.add('is-active')
    }

    const select = document.createElement('button')
    select.className = 'session-select'
    select.type = 'button'
    select.title = session.title
    select.innerHTML = '<i data-lucide="message-square"></i>'

    const copy = document.createElement('span')
    copy.className = 'session-copy'

    const title = document.createElement('strong')
    title.textContent = session.title || '新会话'

    const time = document.createElement('small')
    time.textContent = formatSessionTime(session.updated_at)

    copy.append(title, time)
    select.append(copy)
    select.addEventListener('click', () => void loadSession(session.id))

    const remove = document.createElement('button')
    remove.className = 'session-delete'
    remove.type = 'button'
    remove.title = '删除会话'
    remove.innerHTML = '<i data-lucide="trash-2"></i>'
    remove.addEventListener('click', () => void deleteSession(session))

    row.append(select, remove)
    elements.sessionList.append(row)
  }

  createLucideIcons(elements.sessionList)
}

async function refreshSessionList() {
  const response = await apiRequest('/sessions?limit=50')
  const payload = await response.json()
  state.sessions = Array.isArray(payload.sessions) ? payload.sessions : []
  renderSessionList()
}

function renderDatasetOptions(selectedID = elements.datasetSelect.value) {
  elements.datasetSelect.replaceChildren()

  const builtIn = document.createElement('option')
  builtIn.value = ''
  builtIn.textContent = '内置电商数据'
  elements.datasetSelect.append(builtIn)

  for (const dataset of state.datasets) {
    const option = document.createElement('option')
    option.value = dataset.id
    const sizeLabel =
      dataset.source_type === 'excel'
        ? `${dataset.row_count || 0} 行`
        : `${dataset.chunk_count || 0} 个片段`
    option.textContent = `${dataset.name} · ${sizeLabel}`
    elements.datasetSelect.append(option)
  }

  if ([...elements.datasetSelect.options].some((option) => option.value === selectedID)) {
    elements.datasetSelect.value = selectedID
  } else {
    elements.datasetSelect.value = ''
  }
  updateDatasetActions()
}

function selectedDataset() {
  return state.datasets.find((dataset) => dataset.id === elements.datasetSelect.value) || null
}

function updateDatasetActions() {
  const dataset = selectedDataset()
  elements.datasetActions.hidden = !dataset
  elements.renameDataset.disabled = state.generating || !dataset
  elements.deleteDataset.disabled = state.generating || !dataset
}

async function loadDatasets() {
  try {
    const response = await apiRequest('/datasets')
    const payload = await response.json()
    state.datasets = Array.isArray(payload.datasets) ? payload.datasets : []
    renderDatasetOptions()
  } catch (error) {
    console.error('load datasets:', error)
    updateDatasetActions()
  }
}

function setDatasetError(message = '') {
  elements.datasetError.hidden = !message
  elements.datasetError.textContent = message
}

function openDatasetModal() {
  elements.datasetForm.reset()
  elements.datasetFileName.textContent = '选择数据文件'
  elements.datasetSubmit.disabled = false
  setDatasetError()
  elements.datasetModal.hidden = false
  elements.datasetName.focus()
}

function closeDatasetModal() {
  elements.datasetModal.hidden = true
}

function setRenameDatasetError(message = '') {
  elements.renameDatasetError.hidden = !message
  elements.renameDatasetError.textContent = message
}

function openRenameDatasetModal() {
  const dataset = selectedDataset()
  if (!dataset) {
    return
  }
  setRenameDatasetError()
  elements.renameDatasetName.value = dataset.name
  elements.renameDatasetModal.hidden = false
  elements.renameDatasetName.focus()
  elements.renameDatasetName.select()
}

function closeRenameDatasetModal() {
  elements.renameDatasetModal.hidden = true
}

async function renameSelectedDataset() {
  const dataset = selectedDataset()
  if (!dataset) {
    closeRenameDatasetModal()
    return
  }
  const name = elements.renameDatasetName.value.trim()
  if (!name) {
    setRenameDatasetError('请输入文档名称')
    return
  }

  elements.renameDatasetSubmit.disabled = true
  elements.renameDatasetSubmit.querySelector('span').textContent = '正在保存...'
  setRenameDatasetError()
  try {
    const response = await apiRequest(`/datasets/${encodeURIComponent(dataset.id)}`, {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ name }),
    })
    const payload = await response.json()
    state.datasets = state.datasets.map((item) =>
      item.id === dataset.id ? payload.dataset : item,
    )
    renderDatasetOptions(dataset.id)
    closeRenameDatasetModal()
    setConnectionStatus('online', '名称已更新')
  } catch (error) {
    if (isUnauthorized(error)) {
      return
    }
    setRenameDatasetError(error.message || '重命名失败')
  } finally {
    elements.renameDatasetSubmit.disabled = false
    elements.renameDatasetSubmit.querySelector('span').textContent = '保存名称'
  }
}

async function deleteSelectedDataset() {
  const dataset = selectedDataset()
  if (!dataset || state.generating) {
    return
  }
  const confirmed = window.confirm(
    `确定删除“${dataset.name}”吗？该文档的 ${dataset.chunk_count} 个向量片段将被永久删除。`,
  )
  if (!confirmed) {
    return
  }

  try {
    await apiRequest(`/datasets/${encodeURIComponent(dataset.id)}`, { method: 'DELETE' })
    state.datasets = state.datasets.filter((item) => item.id !== dataset.id)
    renderDatasetOptions('')
    setConnectionStatus('online', '知识库已删除')
  } catch (error) {
    if (isUnauthorized(error)) {
      return
    }
    setConnectionStatus('offline', '删除失败')
  }
}

async function submitDataset() {
  const file = elements.datasetFile.files?.[0]
  if (!file) {
    setDatasetError('请选择数据文件')
    return
  }
  const extension = file.name.toLowerCase().match(/\.[^.]+$/)?.[0] || ''
  if (!['.md', '.markdown', '.txt', '.docx', '.xlsx'].includes(extension)) {
    setDatasetError('当前只支持 .md、.markdown、.txt、.docx 和 .xlsx 文件')
    return
  }
  const isExcel = extension === '.xlsx'

  const name = elements.datasetName.value.trim() || file.name.replace(/\.[^.]+$/, '')
  const duplicateName = state.datasets.some(
    (dataset) => dataset.name.toLocaleLowerCase() === name.toLocaleLowerCase(),
  )
  if (
    duplicateName &&
    !window.confirm('已存在同名数据集，但内容可能不同。仍要上传吗？')
  ) {
    return
  }

  const formData = new FormData()
  formData.set('name', name)
  formData.set('file', file)

  elements.datasetSubmit.disabled = true
  elements.datasetSubmit.querySelector('span').textContent = isExcel
    ? '正在解析并建表...'
    : '正在解析并索引...'
  setDatasetError()
  try {
    const response = await apiRequest(isExcel ? '/datasets/excel' : '/datasets', {
      method: 'POST',
      body: formData,
    })
    const payload = await response.json()
    state.datasets = [payload.dataset, ...state.datasets]
    renderDatasetOptions(payload.dataset.id)
    closeDatasetModal()
    setConnectionStatus('online', isExcel ? 'Excel 数据表已创建' : '文档已建立索引')
  } catch (error) {
    setDatasetError(error.message || '数据文件上传失败')
  } finally {
    elements.datasetSubmit.disabled = false
    elements.datasetSubmit.querySelector('span').textContent = '上传并建立数据集'
  }
}

function clearConversation() {
  elements.messages.replaceChildren()
  showEmptyState(true)
  resetTrace()
}

async function loadLatestAnalysisRun(sessionID) {
  try {
    const response = await apiRequest(
      `/analysis/runs?session_id=${encodeURIComponent(sessionID)}&limit=1`,
    )
    const payload = await response.json()
    const run = payload.runs?.[0]
    if (!run) {
      resetTrace()
      return
    }
    const detailResponse = await apiRequest(`/analysis/runs/${encodeURIComponent(run.id)}`)
    renderRunDetail(await detailResponse.json())
  } catch (error) {
    console.error('load analysis run:', error)
    resetTrace('无法读取该会话的执行轨迹。')
  }
}

async function createSession() {
  stopGeneration()
  setConnectionStatus('connecting', '连接中')

  try {
    const response = await apiRequest('/sessions', { method: 'POST' })
    const payload = await response.json()
    state.sessionId = payload.session_id
    localStorage.setItem(SESSION_STORAGE_KEY, state.sessionId)
    setCurrentTitle('新会话')
    clearConversation()
    setConnectionStatus('online', '已连接')
  } catch (error) {
    console.error('create session:', error)
    if (isUnauthorized(error)) {
      return
    }
    state.sessionId = crypto.randomUUID()
    localStorage.setItem(SESSION_STORAGE_KEY, state.sessionId)
    setCurrentTitle('新会话')
    clearConversation()
    setConnectionStatus('offline', '后端不可用')
  }

  setSidebarOpen(false)
}

async function loadSession(sessionID) {
  stopGeneration()
  setConnectionStatus('connecting', '连接中')

  try {
    const response = await apiRequest(`/sessions/${encodeURIComponent(sessionID)}/messages`)
    const payload = await response.json()
    const messages = Array.isArray(payload.messages) ? payload.messages : []

    state.sessionId = sessionID
    localStorage.setItem(SESSION_STORAGE_KEY, sessionID)
    clearConversation()

    if (messages.length === 0) {
      showEmptyState(true)
    } else {
      for (const message of messages) {
        createMessage(message.role, message.content, { status: message.status })
      }
    }

    const session = state.sessions.find((item) => item.id === sessionID)
    setCurrentTitle(session?.title)
    renderSessionList()
    await loadLatestAnalysisRun(sessionID)
    setConnectionStatus('online', '已连接')
  } catch (error) {
    console.error('load session:', error)
    if (isUnauthorized(error)) {
      return
    }
    setConnectionStatus('offline', '读取失败')
    if (error instanceof ApiError && error.status === 404) {
      await refreshSessionList()
      if (state.sessions.length > 0) {
        await loadSession(state.sessions[0].id)
      } else {
        await createSession()
      }
    }
  } finally {
    setSidebarOpen(false)
  }
}

async function deleteSession(session) {
  const confirmed = window.confirm(`确定删除“${session.title || '新会话'}”吗？`)
  if (!confirmed) {
    return
  }

  stopGeneration()
  try {
    await apiRequest(`/sessions/${encodeURIComponent(session.id)}`, { method: 'DELETE' })
    state.sessions = state.sessions.filter((item) => item.id !== session.id)

    if (state.sessionId === session.id) {
      if (state.sessions.length > 0) {
        await loadSession(state.sessions[0].id)
      } else {
        await createSession()
      }
    } else {
      renderSessionList()
    }
  } catch (error) {
    console.error('delete session:', error)
    if (isUnauthorized(error)) {
      return
    }
    setConnectionStatus('offline', '删除失败')
  }
}

function stopGeneration() {
  const runID = state.activeRunId
  if (state.controller) {
    state.controller.abort()
    state.controller = null
  }
  if (runID && state.generating) {
    void apiRequest(`/analysis/runs/${encodeURIComponent(runID)}/cancel`, {
      method: 'POST',
    }).catch((error) => {
      if (!isUnauthorized(error)) {
        console.error('cancel analysis run:', error)
      }
    })
  }
  setGenerating(false)
}

function parseSSEBlock(block, onEvent) {
  let eventName = 'message'
  const data = []

  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) {
      eventName = line.slice(6).trim()
    } else if (line.startsWith('data:')) {
      data.push(line.slice(5).trimStart())
    }
  }

  if (data.length === 0) {
    return
  }

  const rawData = data.join('\n')
  try {
    onEvent(eventName, JSON.parse(rawData))
  } catch (error) {
    console.error('parse SSE event:', error, rawData)
  }
}

async function consumeEventStream(stream, onEvent) {
  const reader = stream.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) {
      break
    }

    buffer += decoder.decode(value, { stream: true })
    let boundary = buffer.indexOf('\n\n')
    while (boundary !== -1) {
      const block = buffer.slice(0, boundary).replaceAll('\r\n', '\n')
      buffer = buffer.slice(boundary + 2)
      if (block.trim()) {
        parseSSEBlock(block, onEvent)
      }
      boundary = buffer.indexOf('\n\n')
    }
  }

  buffer += decoder.decode()
  if (buffer.trim()) {
    parseSSEBlock(buffer.replaceAll('\r\n', '\n'), onEvent)
  }
}

async function sendMessage(message) {
  if (!message || state.generating) {
    return
  }

  createMessage('user', message)
  const { article: assistantArticle, body: assistantBody } = createMessage(
    'assistant',
    '',
    { loading: true },
  )
  let answer = ''
  let renderFrame = 0
  let finalStatus = 'complete'
  resetTrace('正在创建分析任务...')

  state.controller = new AbortController()
  setGenerating(true)
  setConnectionStatus('busy', '生成中')

  const renderAnswer = () => {
    renderFrame = 0
    assistantBody.innerHTML = renderMarkdown(answer)
    highlightCode(assistantBody)
    if (finalStatus === 'partial') {
      addStatusBadge(assistantBody, finalStatus)
    }
    scrollToBottom()
  }

  try {
    const response = await apiRequest('/analysis/runs', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        session_id: state.sessionId,
        dataset_id: elements.datasetSelect.value,
        question: message,
      }),
      signal: state.controller.signal,
    })
    const run = await response.json()
    state.activeRunId = run.run_id
    state.lastRun = run
    elements.traceSummary.textContent = '分析任务排队中...'
    if (window.matchMedia('(max-width: 820px)').matches) {
      setTraceOpen(true)
    }

    const eventsResponse = await apiRequest(
      `/analysis/runs/${encodeURIComponent(run.run_id)}/events`,
      { signal: state.controller.signal },
    )

    if (!eventsResponse.body) {
      throw new Error('浏览器不支持流式响应')
    }

    let streamError = null
    await consumeEventStream(eventsResponse.body, (eventName, event) => {
      appendTraceEvent(event)
      if (eventName === 'answer.delta') {
        answer += event.data?.content || ''
        if (!renderFrame) {
          renderFrame = requestAnimationFrame(renderAnswer)
        }
      } else if (eventName === 'run.completed') {
        state.lastRun = event.data
        finalStatus = 'complete'
      } else if (eventName === 'run.cancelled') {
        finalStatus = 'partial'
      } else if (eventName === 'run.failed') {
        streamError = new Error(event.data?.error || '分析任务执行失败')
      }
    })

    if (renderFrame) {
      cancelAnimationFrame(renderFrame)
      renderAnswer()
    }
    if (streamError) {
      throw streamError
    }
    if (!answer) {
      const detailResponse = await apiRequest(
        `/analysis/runs/${encodeURIComponent(run.run_id)}`,
      )
      const detail = await detailResponse.json()
      answer = detail.run?.final_answer || ''
      renderRunDetail(detail)
    }
    if (!answer) {
      assistantBody.textContent = '分析任务已完成，但没有生成报告。'
    }
    setConnectionStatus('online', '已连接')
  } catch (error) {
    if (error.name === 'AbortError') {
      finalStatus = answer ? 'partial' : 'complete'
      if (!answer) {
        assistantBody.textContent = '已停止分析。'
      } else {
        assistantBody.innerHTML = renderMarkdown(answer)
        highlightCode(assistantBody)
        addStatusBadge(assistantBody, 'partial')
      }
      setConnectionStatus('online', '已连接')
    } else {
      console.error('chat stream:', error)
      if (isUnauthorized(error)) {
        return
      }
      finalStatus = answer ? 'partial' : 'complete'
      assistantArticle.classList.add('message--error')
      assistantBody.innerHTML = answer
        ? renderMarkdown(answer)
        : error.message || '请求失败，请稍后重试。'
      if (answer) {
        highlightCode(assistantBody)
        addStatusBadge(assistantBody, 'partial')
      }
      setConnectionStatus('offline', '请求失败')
    }
  } finally {
    state.controller = null
    setGenerating(false)

    if (!state.token) {
      return
    }

    try {
      await refreshSessionList()
      const active = state.sessions.find((session) => session.id === state.sessionId)
      setCurrentTitle(active?.title)
    } catch (error) {
      console.error('refresh sessions:', error)
    }
  }
}

elements.composer.addEventListener('submit', (event) => {
  event.preventDefault()
  const message = elements.input.value.trim()
  if (!message) {
    return
  }

  elements.input.value = ''
  resizeInput()
  updateSendButton()
  void sendMessage(message)
})

elements.input.addEventListener('input', () => {
  resizeInput()
  updateSendButton()
})

elements.input.addEventListener('keydown', (event) => {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
    event.preventDefault()
    elements.composer.requestSubmit()
  }
})

elements.stop.addEventListener('click', stopGeneration)
elements.newSession.addEventListener('click', () => void createSession())
elements.newSessionTop.addEventListener('click', () => void createSession())
elements.uploadDataset.addEventListener('click', openDatasetModal)
elements.closeDatasetModal.addEventListener('click', closeDatasetModal)
elements.datasetModalBackdrop.addEventListener('click', closeDatasetModal)
elements.datasetSelect.addEventListener('change', updateDatasetActions)
elements.renameDataset.addEventListener('click', openRenameDatasetModal)
elements.deleteDataset.addEventListener('click', () => void deleteSelectedDataset())
elements.closeRenameDataset.addEventListener('click', closeRenameDatasetModal)
elements.renameDatasetBackdrop.addEventListener('click', closeRenameDatasetModal)
elements.datasetFile.addEventListener('change', () => {
  const file = elements.datasetFile.files?.[0]
  elements.datasetFileName.textContent = file?.name || '选择数据文件'
  setDatasetError()
})
elements.datasetForm.addEventListener('submit', (event) => {
  event.preventDefault()
  void submitDataset()
})
elements.renameDatasetForm.addEventListener('submit', (event) => {
  event.preventDefault()
  void renameSelectedDataset()
})
elements.logout.addEventListener('click', logout)
elements.historyToggle.addEventListener('click', () => setSidebarOpen(true))
elements.closeHistory.addEventListener('click', () => setSidebarOpen(false))
elements.sidebarOverlay.addEventListener('click', () => setSidebarOpen(false))
elements.traceToggle.addEventListener('click', () => setTraceOpen(true))
elements.closeTrace.addEventListener('click', () => setTraceOpen(false))
elements.traceOverlay.addEventListener('click', () => setTraceOpen(false))

document.querySelectorAll('[data-auth-mode]').forEach((button) => {
  button.addEventListener('click', () => setAuthMode(button.dataset.authMode))
})

elements.authForm.addEventListener('submit', (event) => {
  event.preventDefault()
  void submitAuthentication()
})

elements.authPassword.addEventListener('input', () => {
  updatePasswordStrength()
  updatePasswordConfirmation()
})

elements.authPasswordConfirm.addEventListener('input', updatePasswordConfirmation)

document.querySelectorAll('[data-prompt]').forEach((button) => {
  button.addEventListener('click', () => {
    elements.input.value = button.dataset.prompt
    resizeInput()
    updateSendButton()
    elements.input.focus()
  })
})

window.addEventListener('beforeunload', stopGeneration)

async function initializeChat() {
  showAppView()
  try {
    await loadDatasets()
    await refreshSessionList()
    const storedSession = state.sessions.find((session) => session.id === state.sessionId)
    if (storedSession) {
      await loadSession(storedSession.id)
    } else if (state.sessions.length > 0) {
      await loadSession(state.sessions[0].id)
    } else {
      await createSession()
    }
  } catch (error) {
    console.error('initialize chat:', error)
    if (isUnauthorized(error)) {
      return
    }
    setConnectionStatus('offline', '后端不可用')
    await createSession()
  }
}

function initialize() {
  createLucideIcons()
  resizeInput()
  resetTrace()
  localStorage.removeItem('go-agentery.client-id')
  const requestedMode = new URLSearchParams(window.location.search).get('mode')
  setAuthMode(requestedMode === 'register' ? 'register' : 'login')

  if (state.token) {
    showAppView()
    void initializeChat()
  } else {
    showAuthView()
  }
}

void initialize()
