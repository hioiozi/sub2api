import { describe, expect, it } from 'vitest'
import { fitCoverPreview } from '../coverPreview'
import { coverOverflow, draggedCoverPosition } from '../coverDrag'

describe('dashboard-size cover preview', () => {
  it('shrinks both axes uniformly instead of recropping the image', () => {
    expect(fitCoverPreview({ width: 1500, height: 180 }, 750)).toEqual({
      width: 1500, height: 180, scale: 0.5, displayHeight: 90
    })
  })
  it('does not enlarge a narrow dashboard card', () => {
    expect(fitCoverPreview({ width: 500, height: 180 }, 750)?.scale).toBe(1)
  })
  it('waits for valid element measurements', () => {
    for (const width of [0, -1, NaN, Infinity]) {
      expect(fitCoverPreview({ width, height: 180 }, 750)).toBeNull()
      expect(fitCoverPreview({ width: 1500, height: 180 }, width)).toBeNull()
    }
    expect(fitCoverPreview({ width: 1500, height: 0 }, 750)).toBeNull()
  })
  it.each([[600, 1000], [1600, 900], [1800, 400]])('keeps the crop and drag invariant for %ix%i images', (width, height) => {
    const original = coverOverflow(1000, 178, width, height)
    const scale = 0.6
    const preview = coverOverflow(1000 * scale, 178 * scale, width, height)
    expect(preview.x).toBeCloseTo(original.x * scale)
    expect(preview.y).toBeCloseTo(original.y * scale)
    const start = { position_x: 70, position_y: 50 }
    expect(draggedCoverPosition(start, 60 * scale, 30 * scale, preview))
      .toEqual(draggedCoverPosition(start, 60, 30, original))
  })
})
