import '@testing-library/jest-dom/vitest'
import { cleanup, configure } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// jsdom 缺少的浏览器 API，shadcn（sidebar / tooltip）与 recharts 依赖它们
if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia
}

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = vi.fn()
}

// Base UI 的 scroll-area 会调用 getAnimations
if (!Element.prototype.getAnimations) {
  Element.prototype.getAnimations = () => []
}

// 侧边栏折叠状态依赖该值
if (!window.HTMLElement.prototype.hasPointerCapture) {
  window.HTMLElement.prototype.hasPointerCapture = () => false
}

// 首个用例需要动态加载图表分包，默认 1s 不够
configure({ asyncUtilTimeout: 5000 })

afterEach(() => {
  cleanup()
})
