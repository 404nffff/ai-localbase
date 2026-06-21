export const APP_VERSION = (import.meta.env.VITE_APP_VERSION || '').trim()
export const APP_VERSION_LABEL = APP_VERSION || '开发构建'
export const IS_RELEASE_BUILD = Boolean(APP_VERSION)
