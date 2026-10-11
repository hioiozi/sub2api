import { apiClient } from './client'

export interface AccessIPRecord { id: string; ip: string; status: string; created_at: string; updated_at: string }
export interface AccessProtectionState { current_ip: string; quota: number; used: number; total_recharged: number; items: AccessIPRecord[] }
export async function getAccessProtection(): Promise<AccessProtectionState> { return (await apiClient.get<AccessProtectionState>('/user/access-protection')).data }
export async function addAccessIP(ip: string) { return (await apiClient.post<{ id: string }>('/user/access-protection', { ip })).data }
export async function updateAccessIP(id: string, ip: string) { return (await apiClient.put(`/user/access-protection/${encodeURIComponent(id)}`, { ip })).data }
export async function removeAccessIP(id: string) { return (await apiClient.delete(`/user/access-protection/${encodeURIComponent(id)}`)).data }
export async function rotateAccessToken(token: string) { return (await apiClient.put('/user/access-protection/token', { token })).data }
