export const formatSpeed = (bytesPerSecond: number): string => {
  if (bytesPerSecond < 1024) return `${bytesPerSecond.toFixed(0)} B/s`
  if (bytesPerSecond < 1024 * 1024) return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`
  return `${(bytesPerSecond / (1024 * 1024)).toFixed(2)} MB/s`
}

export const formatTime = (seconds: number): string => {
  if (seconds < 60) return `${Math.ceil(seconds)}秒`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}分${Math.ceil(seconds % 60)}秒`
  return `${Math.floor(seconds / 3600)}小时${Math.floor((seconds % 3600) / 60)}分`
}
