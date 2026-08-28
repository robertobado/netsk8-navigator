import { cn } from '@/lib/utils'

interface Props {
  checked: boolean
  onChange: () => void
  label: string
  disabled?: boolean
  /** Tailwind class applied when checked — lets a destructive toggle (e.g. "allow write") use --err instead of the default --brand. */
  activeClassName?: string
}

// Shared on/off pill switch — extracted from MCPControls' inline toggle
// markup once the kubeconfig manager's per-row read/write toggles made the
// same shape appear 4+ times across the app.
export function Switch({ checked, onChange, label, disabled, activeClassName = 'bg-[color:var(--brand)]' }: Readonly<Props>) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={onChange}
      className={cn(
        'relative h-5 w-9 shrink-0 rounded-full transition-colors',
        checked ? activeClassName : 'bg-muted',
        disabled && 'cursor-not-allowed opacity-40',
      )}
    >
      <span className={cn('absolute top-0.5 size-4 rounded-full bg-white shadow transition-all', checked ? 'left-[1.125rem]' : 'left-0.5')} />
    </button>
  )
}
