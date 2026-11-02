import { afterEach, describe, expect, it, vi } from 'vitest'
import { streamPlayground } from './stream'

afterEach(() => vi.unstubAllGlobals())

function mockStream(chunks: string[]) {
  const encoder = new TextEncoder()
  vi.stubGlobal('fetch', vi.fn(async () => new Response(new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
      controller.close()
    },
  }), { headers: { 'Content-Type': 'text/event-stream' } })))
}

describe('试跑流式响应', () => {
  it('跨网络分片解析文本和 DONE，仅正常结束时通知完成', async () => {
    mockStream(['data: {"choices":[{"delta":{"content":"你好"}}]}\r\n\r\n', 'data: [DO', 'NE]\n\n'])
    const onDone = vi.fn()
    const onDelta = vi.fn()
    const onError = vi.fn()
    await streamPlayground({ messages: [] }, { onDone, onDelta, onError })
    expect(onDelta).toHaveBeenCalledWith('你好')
    expect(onDone).toHaveBeenCalledTimes(1)
    expect(onError).not.toHaveBeenCalled()
  })

  it('未收到 DONE 就关闭连接时报告异常，不把半截回答记为完成', async () => {
    mockStream(['data: {"choices":[{"delta":{"content":"部分回答"}}]}\n\n'])
    const onDone = vi.fn()
    const onError = vi.fn()
    await streamPlayground({ messages: [] }, { onDone, onError })
    expect(onDone).not.toHaveBeenCalled()
    expect(onError).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ code: 'UPSTREAM_ERROR' }))
  })

  it('保留服务端的超时事件，不重复报告断流', async () => {
    mockStream(['event: error\ndata: {"error":{"code":"UPSTREAM_TIMEOUT","message":"超时"}}\n\n'])
    const onError = vi.fn()
    const onDone = vi.fn()
    await streamPlayground({ messages: [] }, { onError, onDone })
    expect(onError).toHaveBeenCalledExactlyOnceWith({ code: 'UPSTREAM_TIMEOUT', message: '超时' })
    expect(onDone).not.toHaveBeenCalled()
  })

  it('收到 DONE 后主动取消尚未关闭的响应流', async () => {
    const cancel = vi.fn()
    vi.stubGlobal('fetch', vi.fn(async () => new Response(new ReadableStream({
      start(controller) { controller.enqueue(new TextEncoder().encode('data: [DONE]\n\n')) },
      cancel,
    }))))
    await streamPlayground({ messages: [] }, {})
    expect(cancel).toHaveBeenCalledTimes(1)
  })
})
