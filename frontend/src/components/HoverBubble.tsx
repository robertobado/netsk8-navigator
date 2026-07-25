import { useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

// Hover bubble rendered in a portal so it escapes any table/overflow clipping.
export function HoverBubble({ content, children }: Readonly<{ content: ReactNode; children: ReactNode }>) {
  const ref = useRef<HTMLSpanElement>(null)
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null)
  const show = () => {
    const r = ref.current?.getBoundingClientRect()
    if (r) setPos({ x: r.left, y: r.bottom + 6 })
  }
  return (
    <span ref={ref} className="inline-flex" onMouseEnter={show} onMouseLeave={() => setPos(null)}>
      {children}
      {pos &&
        createPortal(
          <div
            style={{ position: 'fixed', left: pos.x, top: pos.y, zIndex: 60 }}
            className="w-max max-w-sm rounded-lg border bg-popover/95 p-2.5 text-xs shadow-2xl shadow-black/40 backdrop-blur-xl"
          >
            {content}
          </div>,
          document.body,
        )}
    </span>
  )
}
