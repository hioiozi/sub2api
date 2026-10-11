import { describe, expect, it } from 'vitest'
import { coverOverflow, draggedCoverPosition } from '../coverDrag'

describe('cover drag geometry', () => {
  const start = { position_x: 50, position_y: 50 }
  it('moves a portrait image with the pointer, leaving the fitted axis untouched', () => {
    const overflow = coverOverflow(600, 180, 600, 900)
    expect(overflow).toEqual({ x: 0, y: 720 })
    expect(draggedCoverPosition(start, 150, 72, overflow)).toEqual({ position_x: 50, position_y: 40 })
  })
  it('moves a panoramic image horizontally, leaving the fitted axis untouched', () => {
    const overflow = coverOverflow(600, 180, 1800, 180)
    expect(overflow).toEqual({ x: 1200, y: 0 })
    expect(draggedCoverPosition(start, -120, 75, overflow)).toEqual({ position_x: 60, position_y: 50 })
  })
  it('clamps at both edges rather than exposing blank areas', () => {
    expect(draggedCoverPosition(start, 10000, -10000, { x: 200, y: 400 })).toEqual({ position_x: 0, position_y: 100 })
    expect(draggedCoverPosition(start, -10000, 10000, { x: 200, y: 400 })).toEqual({ position_x: 100, position_y: 0 })
  })
  it('keeps a perfect fit and unloaded image stationary', () => {
    expect(draggedCoverPosition(start, 100, 100, coverOverflow(600, 180, 1200, 360))).toEqual(start)
    expect(coverOverflow(600, 180, 0, 0)).toEqual({ x: 0, y: 0 })
  })
  it('returns integer positions compatible with existing API storage', () => {
    expect(draggedCoverPosition(start, 3, -3, { x: 71, y: 123 })).toEqual({ position_x: 46, position_y: 52 })
  })
})
