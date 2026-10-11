import { apiClient } from './client'

export interface DashboardCover {
  version: number
  image_data: string
  position_x: number
  position_y: number
  mask: number
}

export const defaultDashboardCover = (): DashboardCover => ({
  version: 1, image_data: '', position_x: 70, position_y: 50, mask: 78
})

const endpoint = '/user/dashboard-cover'
export async function getDashboardCover(signal?: AbortSignal): Promise<DashboardCover> {
  return (await apiClient.get<DashboardCover>(endpoint, { signal })).data
}
export async function saveDashboardCover(cover: DashboardCover, originalImage?: string): Promise<DashboardCover> {
  const { image_data, position_x, position_y, mask } = cover
  return (await apiClient.put<DashboardCover>(endpoint, {
    image_data: image_data === originalImage ? undefined : image_data, position_x, position_y, mask
  })).data
}

// Client-side convenience only: the server independently validates and re-encodes images.
export async function prepareCoverImage(file: File): Promise<string> {
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) throw new Error('type')
  if (file.size > 5 * 1024 * 1024) throw new Error('size')
  const bitmap = await createImageBitmap(file)
  try {
    if (bitmap.width > 8192 || bitmap.height > 8192 || bitmap.width * bitmap.height > 16000000) {
      throw new Error('dimensions')
    }
    const scale = Math.min(1, 1920 / bitmap.width, 1080 / bitmap.height)
    const canvas = document.createElement('canvas')
    canvas.width = Math.max(1, Math.round(bitmap.width * scale))
    canvas.height = Math.max(1, Math.round(bitmap.height * scale))
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('decode')
    ctx.fillStyle = '#f7f6fb'
    ctx.fillRect(0, 0, canvas.width, canvas.height)
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
    return canvas.toDataURL('image/jpeg', 0.85)
  } finally { bitmap.close() }
}
