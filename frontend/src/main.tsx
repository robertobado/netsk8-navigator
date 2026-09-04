import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import './index.css'
import App from './App.tsx'
import { hydrateAppPrefs } from './lib/preferences'
import { hydrateMcpGate } from './lib/mcpGate'

// Pull server-side preferences once at startup (localStorage is the sync source).
hydrateAppPrefs()
// The /mcp security gate has its own store; the backend is authoritative for it.
hydrateMcpGate()

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Live-ish feel: refetch on an interval, keep stale data while refreshing.
      refetchInterval: 10_000,
      staleTime: 5_000,
      retry: 1,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
