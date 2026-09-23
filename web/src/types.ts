export type User = { id: string; email: string; created_at: string }

export type Monitor = {
  id: string
  project_id: string
  type: 'http' | 'tcp' | 'heartbeat'
  name: string
  url: string
  target?: string
  check_interval_seconds: number
  timeout_seconds: number
  enabled: boolean
  last_checked_at: string | null
  last_available: boolean | null
  last_status_code: number | null
  last_response_time_ms: number | null
  last_error: string | null
  heartbeat_token?: string
}

export type Project = {
  id: string
  name: string
  description: string
  monitors: Monitor[]
  created_at: string
  updated_at: string
}

export type Check = {
  id: number
  checked_at: string
  available: boolean
  status_code: number | null
  response_time_ms: number | null
  error: string | null
}

export type Incident = {
  id: string
  started_at: string
  resolved_at: string | null
  duration_seconds: number
  cause: string | null
}

export type OpenIncident = Incident & {
  project_id: string
  project_name: string
  monitor_id: string
  monitor_name: string
}

export type Summary = {
  from: string
  to: string
  status: string
  uptime_percent: number | null
  coverage_percent: number
  observed_duration_seconds: number
  available_duration_seconds: number
  check_count: number
  average_response_time_ms: number | null
  peak_response_time_ms: number | null
  last_incident: Incident | null
  generated_at: string
}

export type Webhook = {
  id: string
  project_id: string
  url: string
  events: string
  enabled: boolean
  secret?: string
}

export type NotificationAttempt = {
  attempt: number
  success: boolean
  error_code: string | null
  completed_at: string
}

export type Notification = {
  id: string
  kind: string
  monitor_name: string
  status: string
  attempts: number
  last_error: string | null
  occurred_at: string
  attempt_history: NotificationAttempt[]
}
