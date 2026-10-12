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
  attempts?: number
  retry_at?: string
  cooldown_until?: string
  duration_ms: number
  created_at: string
}

export interface DetectionState {
  cooldowns?: Record<string, string>
  generated: number
  failed: number
  items: DetectionRecord[]
}

export async function listDetections(): Promise<DetectionState> {
  const { data } = await apiClient.get<DetectionState>('/user/model-detection')
  return data
}

export async function startDetection(input: { group_id: number; model: string; topic: string; prompt: string }): Promise<DetectionRecord> {
  const { data } = await apiClient.post<DetectionRecord>('/user/model-detection', input)
  return data
}

export async function deleteDetection(id: string): Promise<void> {
  await apiClient.delete(`/user/model-detection/${encodeURIComponent(id)}`)
}

export interface DetectionPresets { version: number; pelican: string; candy: string }
export interface DetectionGroup { id: number; name: string; platform: string; models: string[] }
export interface DetectionConfig { groups: DetectionGroup[]; presets: DetectionPresets }
export async function getDetectionConfig(): Promise<DetectionConfig> {
  const { data } = await apiClient.get<DetectionConfig>('/user/model-detection/config')
  return data
}
export async function saveDetectionPresets(input: DetectionPresets): Promise<DetectionPresets> {
  const { data } = await apiClient.put<DetectionPresets>('/user/model-detection/presets', input)
  return data
}
