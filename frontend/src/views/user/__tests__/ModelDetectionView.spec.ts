import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ModelDetectionView from '../ModelDetectionView.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), records: vi.fn(), start: vi.fn() }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: api.list } }))
vi.mock('@/api/client', () => ({ buildGatewayUrl: (path: string) => path }))
vi.mock('@/api/modelDetection', () => ({ listDetections: api.records, startDetection: api.start, deleteDetection: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 1, balance: 10 }, isAdmin: false }) }))

let wrapper: VueWrapper
const fetchMock = vi.fn()
const response = (ids: string[]) => ({ ok: true, json: async () => ({ data: ids.map(id => ({ id })) }) })
async function openModel() {
  await wrapper.get('#model-select').trigger('click')
  await flushPromises()
}
describe('model detection group and model selection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('fetch', fetchMock)
    api.list.mockResolvedValue({ total: 2, items: [1, 2].map(id => ({ id, group_id: id, group: { name: `Group ${id}` }, key: `key-${id}`, status: 'active' })) })
    api.records.mockResolvedValue({ generated: 0, failed: 0, items: [] })
    fetchMock.mockResolvedValue(response(['codex-auto-review', 'gpt-6-astra', 'gpt-6-sol']))
    wrapper = mount(ModelDetectionView, { attachTo: document.body, global: {
      plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { common: { searchPlaceholder: '搜索', selectOption: '请选择', noOptionsFound: '无结果' } } } })],
      stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true }
    } })
  })
  afterEach(() => { wrapper.unmount(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

  it('opens the full list without prefiltering by the first model and submits the chosen ID', async () => {
    await flushPromises()
    expect(wrapper.get('#model-select').text()).toContain('选择或输入模型 ID')
    await openModel()
    const options = [...document.querySelectorAll('[role=option]')]
    expect(options.map(el => el.textContent?.trim())).toEqual(['codex-auto-review', 'gpt-6-astra', 'gpt-6-sol'])
    ;(options[1] as HTMLElement).click()
    await flushPromises()
    api.start.mockRejectedValue(new Error('fixture: no paid request'))
    await wrapper.get('.run-button').trigger('click')
    expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ key_id: 1, model: 'gpt-6-astra' }))
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer key-1')
  })

  it('clears selection on group change and ignores a late result from the previous group', async () => {
    await flushPromises()
    let finishOld!: (value: unknown) => void
    fetchMock.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
    fetchMock.mockResolvedValueOnce(response(['current-group-model']))
    const selects = wrapper.findAllComponents({ name: 'Select' })
    selects[0].vm.$emit('update:modelValue', '2')
    await flushPromises()
    selects[1].vm.$emit('update:modelValue', 'stale-model')
    selects[0].vm.$emit('update:modelValue', '1')
    await flushPromises()
    finishOld(response(['wrong-group-model']))
    await flushPromises()
    expect(wrapper.get('#model-select').text()).toContain('选择或输入模型 ID')
    await openModel()
    expect(document.querySelector('[role=listbox]')?.textContent).toContain('current-group-model')
    expect(document.querySelector('[role=listbox]')?.textContent).not.toContain('wrong-group-model')
  })

  it('allows manual model ID input when discovery fails', async () => {
    await flushPromises()
    fetchMock.mockRejectedValueOnce(new Error('unavailable'))
    wrapper.findAllComponents({ name: 'Select' })[0].vm.$emit('update:modelValue', '2')
    await flushPromises()
    expect(wrapper.text()).toContain('模型列表读取失败')
    await openModel()
    const input = document.querySelector('.select-search-input') as HTMLInputElement
    input.value = 'custom-model'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    ;(document.querySelector('[role=option]') as HTMLElement).click()
    await flushPromises()
    expect(wrapper.get('#model-select').text()).toContain('custom-model')
  })
})
