import { afterEach, describe, expect, it, vi } from 'vitest'
import { setApiAccessToken, updateWikiQueue } from './api'

afterEach(() => {
  vi.unstubAllGlobals()
  setApiAccessToken('')
})

describe('整理队列设置接口', () => {
  it('只发送修改字段并复用现有鉴权，采用服务端回执', async () => {
    const queue = { paused: true, concurrency: 3 }
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(queue)))
    vi.stubGlobal('fetch', fetchMock)
    setApiAccessToken('queue-fixture')
    await expect(updateWikiQueue({ paused: true })).resolves.toEqual(queue)
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toContain('/api/wiki/queue')
    expect(options.method).toBe('PUT')
    expect(JSON.parse(options.body)).toEqual({ paused: true })
    expect(new Headers(options.headers).get('Authorization')).toBe('Bearer queue-fixture')
  })

  it('并发修改不携带暂停状态，保存错误直接反馈', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: '保存整理队列设置失败' }), { status: 500 }))
    vi.stubGlobal('fetch', fetchMock)
    await expect(updateWikiQueue({ concurrency: 2 })).rejects.toThrow('保存整理队列设置失败')
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ concurrency: 2 })
  })
})
