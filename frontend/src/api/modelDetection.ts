import { apiClient } from './client'

export interface DetectionRecord {
  id: string
  user_id: number
  group_id: number
  platform: string
  key_name: string
  model: string
  topic: string
  prompt: string
  html: string
  text: string
  status: 'generated' | 'failed' | 'running'
  error?: string
  duration_ms: number
  created_at: string
}

export interface DetectionState {
  generated: number
  failed: number
  items: DetectionRecord[]
}

export async function listDetections(): Promise<DetectionState> {
  const { data } = await apiClient.get<DetectionState>('/user/model-detection')
  return data
}

export async function startDetection(input: { key_id: number; model: string; topic: string; prompt: string }): Promise<DetectionRecord> {
  const { data } = await apiClient.post<DetectionRecord>('/user/model-detection', input)
  return data
}

export async function deleteDetection(id: string): Promise<void> {
  await apiClient.delete(`/user/model-detection/${encodeURIComponent(id)}`)
}
