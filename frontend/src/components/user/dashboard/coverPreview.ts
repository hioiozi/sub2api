export interface CoverSize { width: number; height: number }

// Render at the dashboard's real dimensions, then shrink the whole canvas.
// Reflowing a narrower card at the same height would change object-fit cropping.
export function fitCoverPreview(card: CoverSize, availableWidth: number) {
  if (![card.width, card.height, availableWidth].every(n => Number.isFinite(n) && n > 0)) return null
  const scale = Math.min(1, availableWidth / card.width)
  return { width: card.width, height: card.height, scale, displayHeight: card.height * scale }
}
