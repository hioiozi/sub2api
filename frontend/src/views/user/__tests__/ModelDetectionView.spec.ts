import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ModelDetectionView from '../ModelDetectionView.vue'

const api = vi.hoisted(() => ({ config: vi.fn(), records: vi.fn(), start: vi.fn(), save: vi.fn(), admin: false }))
vi.mock('@/api/modelDetection', () => ({ getDetectionConfig: api.config, saveDetectionPresets: api.save, listDetections: api.records, startDetection: api.start, deleteDetection: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 1, balance: 10 }, isAdmin: api.admin }) }))
let wrapper: VueWrapper
const fetchMock = vi.fn()
const mountPage = () => mount(ModelDetectionView, { attachTo: document.body, global: {
  plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh: { common: { searchPlaceholder: '搜索', selectOption: '请选择', noOptionsFound: '无结果' } } } })],
  stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true, ModelDetectionPreview: true }
} })

describe('model detection configured catalog', () => {
  beforeEach(() => {
    vi.clearAllMocks(); api.admin = false
    vi.stubGlobal('fetch', fetchMock)
    api.config.mockResolvedValue({ groups: [{ id: 1, name: 'Group 1', platform: 'openai', models: ['gpt-6-astra', 'configured-model'] }, { id: 2, name: 'Group 2', platform: 'anthropic', models: ['claude-model'] }], presets: { version: 1, pelican: '创建一个html，内容是SVG绘制鹈鹕骑自行车的2d动画。', candy: '原始糖果问题' } })
    api.records.mockResolvedValue({ generated: 0, failed: 0, items: [], cooldowns: {} })
    wrapper = mountPage()
  })
  afterEach(() => { wrapper.unmount(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

  it('uses configured models without fetching API keys or the gateway catalog', async () => {
    await flushPromises()
    await wrapper.get('#model-select').trigger('click'); await flushPromises()
    const options = [...document.querySelectorAll('[role=option]')]
    expect(options.map(el => el.textContent?.trim())).toEqual(['gpt-6-astra', 'configured-model'])
    ;(options[1] as HTMLElement).click(); await flushPromises()
    api.start.mockRejectedValue(new Error('fixture: no paid request'))
    await wrapper.get('.run-button').trigger('click')
    expect(api.start).toHaveBeenCalledWith(expect.objectContaining({ group_id: 1, model: 'configured-model' }))
    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('先在 API 密钥页面')
  })
  it('clears the model on group changes and shows the configured platform models', async () => {
    await flushPromises()
    const selects = wrapper.findAllComponents({ name: 'Select' })
    selects[1].vm.$emit('update:modelValue', 'gpt-6-astra')
    selects[0].vm.$emit('update:modelValue', '2'); await flushPromises()
    expect(wrapper.get('#model-select').text()).toContain('选择或输入模型 ID')
    await wrapper.get('#model-select').trigger('click'); await flushPromises()
    expect(document.querySelector('[role=listbox]')?.textContent).toContain('claude-model')
    expect(document.querySelector('[role=listbox]')?.textContent).not.toContain('gpt-6-astra')
  })
  it('enforces the server cooldown even when no work remains', async () => {
    wrapper.unmount()
    api.records.mockResolvedValue({ generated: 0, failed: 1, items: [], cooldowns: { '1': new Date(Date.now() + 60000).toISOString() } })
    wrapper = mountPage(); await flushPromises()
    wrapper.findAllComponents({ name: 'Select' })[1].vm.$emit('update:modelValue', 'gpt-6-astra'); await flushPromises()
    expect(wrapper.get('.run-button').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('冷却')
    expect(api.start).not.toHaveBeenCalled()
  })
  it('only shows preset editing for administrators and saves the exact text', async () => {
    await flushPromises(); expect(wrapper.find('#pelican-preset').exists()).toBe(false)
    wrapper.unmount(); api.admin = true; wrapper = mountPage(); await flushPromises()
    await wrapper.get('#pelican-preset').setValue('新预设，原样保留。')
    api.save.mockResolvedValue({ version: 2, pelican: '新预设，原样保留。', candy: '原始糖果问题' })
    await wrapper.get('.preset-editor button').trigger('click'); await flushPromises()
    expect(api.save).toHaveBeenCalledWith({ version: 1, pelican: '新预设，原样保留。', candy: '原始糖果问题' })
    expect(wrapper.get('.prompt-display').text()).toContain('新预设，原样保留。')
  })
})
