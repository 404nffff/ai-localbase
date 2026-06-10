import { useContext } from 'react'
import { ToastContext } from './ToastContainer'

export function useToast() {
  const context = useContext(ToastContext)

  if (!context) {
    throw new Error('useToast must be used within ToastContainer')
  }

  return context
}
