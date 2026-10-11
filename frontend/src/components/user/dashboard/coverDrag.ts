export interface CoverPosition { position_x: number; position_y: number }
export interface CoverOverflow { x: number; y: number }

// object-fit: cover places the image at -(renderedSize - frameSize) * position / 100.
// Invert that relationship so the visible image follows the pointer, without blank edges.
export function coverOverflow(width: number, height: number, naturalWidth: number, naturalHeight: number): CoverOverflow {
  if (![width, height, naturalWidth, naturalHeight].every(n => Number.isFinite(n) && n > 0)) return { x: 0, y: 0 }
  const scale = Math.max(width / naturalWidth, height / naturalHeight)
  return { x: Math.max(0, naturalWidth * scale - width), y: Math.max(0, naturalHeight * scale - height) }
}

export function draggedCoverPosition(start: CoverPosition, deltaX: number, deltaY: number, overflow: CoverOverflow): CoverPosition {
  const axis = (value: number, delta: number, extra: number) => extra > 0.5
    ? Math.max(0, Math.min(100, Math.round(value - delta / extra * 100)))
    : value
  return { position_x: axis(start.position_x, deltaX, overflow.x), position_y: axis(start.position_y, deltaY, overflow.y) }
}
