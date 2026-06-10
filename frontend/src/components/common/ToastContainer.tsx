import { createContext, useCallback, useState, ReactNode } from 'react'

export type ToastType = 'success' | 'error' | 'warning' | 'info'

export interface ToastOptions {
  type: ToastType
  message: string
  duration?: number
}

interface ToastItem extends ToastOptions {
  id: string
  duration: number
}

interface ToastContextValue {
  showToast: (options: ToastOptions) => void
}

export const ToastContext = createContext<ToastContextValue | null>(null)

interface ToastContainerProps {
  children: ReactNode
}

// Legacy compatibility component - deprecated, use ToastProvider from Toast.tsx instead
export default function ToastContainer({ children }: ToastContainerProps) {
  const [toasts, setToasts] = useState<ToastItem[]>([])

  const showToast = useCallback((options: ToastOptions) => {
    const id = `toast-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`
    const duration = options.duration ?? 3000

    setToasts((prev) => [...prev, { ...options, id, duration }])
  }, [])

  const handleClose = useCallback((id: string) => {
    setToasts((prev) => prev.filter((toast) => toast.id !== id))
  }, [])

  return (
    <ToastContext.Provider value={{ showToast }}>
      {children}
      <div className="toast-container">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={`toast toast-${toast.type}`}
            onClick={() => handleClose(toast.id)}
          >
            {toast.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}
