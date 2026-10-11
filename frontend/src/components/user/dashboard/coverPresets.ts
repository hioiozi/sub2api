export const coverPresets = [
  { id: 'sakura', label: '樱花晴空', sky: ['#f9d7e5', '#b8b4dc'], sun: '#fff0d2', back: '#bba5ce', front: '#967db7', bloom: '#f7c7dc' },
  { id: 'coast', label: '海岸微风', sky: ['#bce7e6', '#d8eff3'], sun: '#fff3cd', back: '#83c8cb', front: '#4b9eae', bloom: '#f8ddd5' },
  { id: 'dusk', label: '暮色花园', sky: ['#9b9bc4', '#e3bad0'], sun: '#ffe2b9', back: '#8d8bb3', front: '#5f709a', bloom: '#efc0ce' },
] as const

export type CoverPreset = (typeof coverPresets)[number]

export function renderCoverPreset(preset: CoverPreset): string {
  const canvas = document.createElement('canvas')
  canvas.width = 1440
  canvas.height = 810
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('canvas')
  const sky = ctx.createLinearGradient(0, 0, 0, canvas.height)
  sky.addColorStop(0, preset.sky[0])
  sky.addColorStop(1, preset.sky[1])
  ctx.fillStyle = sky
  ctx.fillRect(0, 0, canvas.width, canvas.height)

  ctx.fillStyle = preset.sun
  ctx.beginPath()
  ctx.arc(1080, 225, 110, 0, Math.PI * 2)
  ctx.fill()

  ctx.fillStyle = preset.back
  ctx.beginPath()
  ctx.moveTo(0, 660)
  ctx.bezierCurveTo(280, 400, 440, 620, 670, 510)
  ctx.bezierCurveTo(920, 360, 1150, 565, 1440, 425)
  ctx.lineTo(1440, 810)
  ctx.lineTo(0, 810)
  ctx.fill()

  ctx.fillStyle = preset.front
  ctx.beginPath()
  ctx.moveTo(0, 745)
  ctx.bezierCurveTo(300, 570, 460, 790, 770, 645)
  ctx.bezierCurveTo(1040, 510, 1220, 745, 1440, 585)
  ctx.lineTo(1440, 810)
  ctx.lineTo(0, 810)
  ctx.fill()

  ctx.fillStyle = preset.bloom
  for (let i = 0; i < 32; i++) {
    const x = 790 + ((i * 199) % 640)
    const y = 55 + ((i * 137) % 340)
    const size = 6 + (i % 5) * 4
    ctx.beginPath()
    ctx.ellipse(x, y, size, size * 0.53, (i % 7) * 0.42, 0, Math.PI * 2)
    ctx.fill()
  }
  return canvas.toDataURL('image/jpeg', 0.82)
}
