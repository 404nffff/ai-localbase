import fs from 'node:fs'
import path from 'node:path'
import { gzipSync } from 'node:zlib'

// 使用 Vite 的真实静态依赖图统计首屏，不把动态 import 计入首屏。
const root = path.resolve('dist')
const manifest = JSON.parse(fs.readFileSync(path.join(root, '.vite/manifest.json'), 'utf8'))
const entries = Object.keys(manifest).filter((key) => manifest[key].isEntry)
if (!entries.length) throw new Error('构建清单缺少入口')
const initial = new Set()
function visit(key) {
  if (initial.has(key)) return
  if (!manifest[key]) throw new Error(`构建清单缺少依赖：${key}`)
  initial.add(key)
  for (const dependency of manifest[key].imports ?? []) visit(dependency)
}
entries.forEach(visit)
const metrics = Object.entries(manifest).filter(([, value]) => value.file.endsWith('.js')).map(([key, value]) => {
  const body = fs.readFileSync(path.join(root, value.file))
  return { file: value.file, initial: initial.has(key), entry: entries.includes(key), raw: body.length, gzip: gzipSync(body).length }
})
const entryGzip = metrics.filter((item) => item.entry).reduce((sum, item) => sum + item.gzip, 0)
const initialGzip = metrics.filter((item) => item.initial).reduce((sum, item) => sum + item.gzip, 0)
const failures = []
if (entryGzip > 100 * 1024) failures.push('入口 gzip 超过 100 KiB')
if (initialGzip > 200 * 1024) failures.push('首屏 JS gzip 超过 200 KiB')
for (const item of metrics) {
  // Mermaid 核心及 Cynefin 是上游不可继续拆分的异步包，保留明确且有上限的例外。
  const chartException = /\/(mermaid\.core|cynefin)-/.test(item.file) && !item.initial
  const limit = chartException ? 750 * 1024 : 500 * 1024
  if (item.raw > limit) failures.push(`${item.file} 超过 ${limit / 1024} KiB 原始大小预算`)
  if (item.initial && /(mermaid|cytoscape|diagram|cynefin)/i.test(item.file)) failures.push(`图表包进入首屏：${item.file}`)
}
console.log(JSON.stringify({ entryGzip, initialGzip, initialFiles: metrics.filter((item) => item.initial), failures }, null, 2))
if (failures.length) process.exitCode = 1
