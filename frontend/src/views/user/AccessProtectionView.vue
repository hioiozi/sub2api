<template>
  <AppLayout>
    <div class="access-page" :aria-busy="loading || busy">
      <header class="access-heading">
        <div><h1>{{ t('accessProtection.heading') }}</h1><p>{{ t('accessProtection.subtitle') }}</p></div>
        <button class="access-refresh" type="button" :disabled="loading || busy" @click="load">{{ t('accessProtection.refresh') }}</button>
      </header>
      <p v-if="error" class="access-alert" role="alert">{{ error }}</p>
      <p v-if="notice" class="access-notice" role="status">{{ notice }}</p>
      <p v-if="loading" role="status">{{ t('accessProtection.fetching') }}</p>
      <template v-if="loaded">
        <section class="access-stats">
          <article><span>{{ t('accessProtection.totalRecharged') }}</span><strong>{{ state.total_recharged.toFixed(2) }}</strong><small>{{ t('accessProtection.quotaHint') }}</small></article>
          <article><span>{{ t('accessProtection.quota') }}</span><strong>{{ state.quota }}</strong><small>{{ t('accessProtection.usedHint') }}</small></article>
          <article><span>{{ t('accessProtection.used') }}</span><strong>{{ state.used }}</strong><small>{{ t('accessProtection.syncedHint') }}</small></article>
          <article class="access-stat-good"><span>{{ t('accessProtection.remaining') }}</span><strong>{{ Math.max(0, state.quota - state.used) }}</strong><small>{{ t('accessProtection.remainingHint') }}</small></article>
        </section>
        <p class="access-accounting">{{ t('accessProtection.accounting') }}</p>
        <div class="access-columns">
          <section class="access-card access-submit">
            <div class="access-card-title"><span class="access-icon" aria-hidden="true">⌁</span><div><h2>{{ t('accessProtection.submitTitle') }}</h2><p>{{ currentEmail }}</p></div></div>
            <div class="access-current"><div><span>{{ t('accessProtection.currentIP') }}</span><code>{{ state.current_ip || t('accessProtection.ipUnavailable') }}</code></div><button type="button" :disabled="busy || !state.current_ip" @click="ip = state.current_ip">{{ t('accessProtection.useCurrent') }}</button></div>
            <p class="access-note">{{ t('accessProtection.currentHint') }}</p>
            <form @submit.prevent="submit">
              <label class="access-label" for="access-ip">{{ t('accessProtection.inputLabel') }}
                <input id="access-ip" v-model.trim="ip" :disabled="busy" :placeholder="t('accessProtection.inputPlaceholder')" type="text" autocomplete="off" spellcheck="false" maxlength="45" required />
              </label>
              <button class="access-submit-button" type="submit" :disabled="busy || !ip || (!editingId && state.used >= state.quota)">{{ busy ? t('accessProtection.loading') : editingId ? t('accessProtection.update') : t('accessProtection.submit') }}</button>
              <button v-if="editingId" class="access-cancel" type="button" :disabled="busy" @click="cancelEdit">{{ t('accessProtection.cancel') }}</button>
            </form>
          </section>
          <section class="access-card access-instructions"><h2>{{ t('accessProtection.instructions') }}</h2><div v-for="(step,index) in steps" :key="index" class="access-step"><b>{{ index + 1 }}</b><div><strong>{{ step[0] }}</strong><p>{{ step[1] }}</p></div></div></section>
        </div>
        <section class="access-card access-history">
          <h2>{{ t('accessProtection.history') }}</h2><p>{{ t('accessProtection.historyHint') }}</p>
          <div v-if="state.items.length" class="access-table" role="table" :aria-label="t('accessProtection.history')">
            <div class="access-row access-row-head" role="row"><span role="columnheader">{{ t('accessProtection.ip') }}</span><span role="columnheader">{{ t('accessProtection.status') }}</span><span role="columnheader">{{ t('accessProtection.created') }}</span><span role="columnheader">{{ t('accessProtection.action') }}</span></div>
            <div v-for="item in state.items" :key="item.id" class="access-row" role="row">
              <code role="cell">{{ item.ip }}</code><span role="cell" class="access-status">{{ t('accessProtection.applied') }}</span><time role="cell" :datetime="item.updated_at">{{ formatDate(item.updated_at) }}</time>
              <span class="access-actions" role="cell"><button type="button" :disabled="busy" @click="edit(item)">{{ t('accessProtection.update') }}</button><button type="button" :disabled="busy" :aria-label="t('accessProtection.removeLabel', { ip: item.ip })" @click="remove(item)">{{ t('accessProtection.remove') }}</button></span>
            </div>
          </div>
          <p v-else class="access-empty">{{ t('accessProtection.empty') }}</p>
        </section>
        <section v-if="auth.isAdmin" class="access-card access-token">
          <h2>{{ t('accessProtection.tokenTitle') }}</h2>
          <p>{{ t('accessProtection.tokenHint') }}</p>
          <form @submit.prevent="rotateToken">
            <label class="access-label" for="access-token">{{ t('accessProtection.tokenLabel') }}
              <input id="access-token" v-model="newToken" :disabled="busy" type="password" autocomplete="new-password" spellcheck="false" maxlength="512" required />
            </label>
            <button class="access-submit-button" type="submit" :disabled="busy || !newToken">{{ busy ? t('accessProtection.loading') : t('accessProtection.tokenRotate') }}</button>
          </form>
        </section>
      </template>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { addAccessIP, getAccessProtection, removeAccessIP, rotateAccessToken, updateAccessIP, type AccessIPRecord, type AccessProtectionState } from '@/api/accessProtection'
import { useAuthStore } from '@/stores/auth'
const { t } = useI18n()
const auth = useAuthStore()
const currentEmail = computed(() => auth.user?.email || '')
const loading = ref(false), loaded = ref(false), busy = ref(false)
const error = ref(''), notice = ref(''), ip = ref(''), editingId = ref('')
const newToken = ref('')
const state = ref<AccessProtectionState>({ current_ip: '', quota: 0, used: 0, total_recharged: 0, items: [] })
const steps = computed(() => [[t('accessProtection.step1Title'),t('accessProtection.step1')],[t('accessProtection.step2Title'),t('accessProtection.step2')],[t('accessProtection.step3Title'),t('accessProtection.step3')]])
async function load() {
  loading.value = true; loaded.value = false; error.value = ''
  try { state.value = await getAccessProtection(); loaded.value = true }
  catch { error.value = t('accessProtection.loadError') }
  finally { loading.value = false }
}
function cancelEdit() { editingId.value = ''; ip.value = '' }
async function edit(item: AccessIPRecord) {
  if (busy.value) return
  editingId.value = item.id; ip.value = item.ip; notice.value = ''
  await nextTick(); document.getElementById('access-ip')?.focus()
}
function formatDate(value: string) { return new Intl.DateTimeFormat(undefined,{dateStyle:'medium',timeStyle:'short'}).format(new Date(value)) }
function message(e: unknown) {
  return (e as { response?: { data?: { message?: string } } })?.response?.data?.message || t('accessProtection.actionError')
}
async function submit() {
  if (busy.value || !loaded.value || !ip.value) return
  busy.value = true; error.value = ''; notice.value = ''
  try {
    if (editingId.value) await updateAccessIP(editingId.value, ip.value)
    else await addAccessIP(ip.value)
    cancelEdit(); await load(); notice.value = t('accessProtection.saved')
  } catch (e) { error.value = message(e) }
  finally { busy.value = false }
}
async function remove(item: AccessIPRecord) {
  if (busy.value || !window.confirm(t('accessProtection.removeConfirm', { ip: item.ip }))) return
  busy.value = true; error.value = ''; notice.value = ''
  try { await removeAccessIP(item.id); if (editingId.value === item.id) cancelEdit(); await load(); notice.value = t('accessProtection.removed') }
  catch (e) { error.value = message(e) }
  finally { busy.value = false }
}
async function rotateToken() {
  if (busy.value || !auth.isAdmin || !newToken.value || !window.confirm(t('accessProtection.tokenConfirm'))) return
  busy.value = true; error.value = ''; notice.value = ''
  try { await rotateAccessToken(newToken.value); newToken.value = ''; notice.value = t('accessProtection.tokenSaved') }
  catch (e) { error.value = message(e) }
  finally { busy.value = false }
}
onMounted(load)
</script>
<style scoped>

.access-page{max-width:1200px;margin:0 auto;display:grid;gap:18px;color:#172238}.access-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.access-heading h1{font-size:26px;font-weight:700}.access-heading p,.access-card>p,.access-step p{margin-top:5px;color:#63728b;font-size:13px}.access-refresh,.access-current button,.access-row button{border:1px solid #d8e0eb;background:#fff;border-radius:9px;padding:9px 15px;font-size:13px}.access-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:16px}.access-stats article{border:1px solid #dce4ee;border-radius:14px;background:#fff;padding:18px;box-shadow:0 2px 8px #4050700d}.access-stats span,.access-stats small{display:block;color:#63728b;font-size:13px}.access-stats strong{display:block;font-size:28px;margin:5px 0;color:#1b2638}.access-stats small{font-size:11px}.access-stat-good{border-color:#91e4dc!important;background:#f1fffc!important}.access-stat-good strong{color:#0d8d83}.access-columns{display:grid;grid-template-columns:1.6fr 1fr;gap:16px}.access-card{border:1px solid #dce4ee;border-radius:14px;background:#fff;padding:22px;box-shadow:0 2px 8px #4050700d}.access-card-title{display:flex;align-items:center;gap:12px}.access-card-title h2,.access-instructions h2,.access-history h2{font-size:18px;font-weight:700}.access-card-title p{font-size:13px;color:#74839b}.access-icon{display:grid;place-items:center;width:42px;height:42px;border-radius:12px;color:#087d76;background:#d9fffa;font-size:26px}.access-current{display:flex;align-items:center;justify-content:space-between;margin-top:22px;border:1px solid #8de7df;background:#effffc;border-radius:10px;padding:12px}.access-current label,.access-label{display:block;color:#65738a;font-size:12px}.access-current code{display:block;margin-top:5px;color:#172238}.access-note{background:#f6f8fb;padding:10px;border-radius:8px}.access-label{margin-top:16px}.access-label input{display:block;width:100%;margin-top:8px;border:1px solid #d7e0eb;border-radius:9px;padding:12px;font-size:14px;outline:none}.access-label input:focus{border-color:#14afa2;box-shadow:0 0 0 3px #14afa220}.access-submit-button{width:100%;margin-top:16px;border:0;border-radius:8px;padding:12px;background:#0d8279;color:#fff;font-weight:600}.access-submit-button:disabled{opacity:.5}.access-step{display:flex;gap:12px;margin-top:18px}.access-step b{display:grid;place-items:center;width:24px;height:24px;border:1px solid #2cc4ba;border-radius:50%;color:#0c9489;font-size:12px;flex:none}.access-step strong{font-size:14px}.access-history>p{margin-bottom:16px}.access-table{border:1px solid #dce4ee;border-radius:10px;overflow:hidden}.access-row{display:grid;grid-template-columns:1.2fr .9fr 1fr .7fr;align-items:center;gap:12px;padding:14px;border-top:1px solid #e6ebf1;font-size:13px}.access-row-head{border-top:0;background:#f7f9fc;color:#63728b;font-size:12px}.access-actions{display:flex;gap:6px;justify-content:flex-end}.access-actions button:last-child{color:#bd4944}.access-status{color:#0b987f;background:#e5faf5;border-radius:20px;padding:3px 9px;width:max-content}.access-empty{text-align:center;padding:38px}.access-alert{padding:12px;border-radius:9px;background:#fff1f0;color:#c23b35;font-size:13px}@media(max-width:800px){.access-stats{grid-template-columns:repeat(2,1fr)}.access-columns{grid-template-columns:1fr}.access-row{grid-template-columns:1fr 1fr}.access-row-head{display:none}}.dark .access-page{color:#f1edf8}.dark .access-card,.dark .access-stats article,.dark .access-refresh,.dark .access-current button,.dark .access-row button{background:#211c2b;border-color:#493d59}.dark .access-heading p,.dark .access-card>p,.dark .access-step p,.dark .access-stats span,.dark .access-stats small,.dark .access-card-title p,.dark .access-current label,.dark .access-label{color:#bdb1cb}.dark .access-stats strong{color:#f1edf8}.dark .access-note,.dark .access-row-head{background:#2b2436}.dark .access-label input{background:#2b2436;color:#fff;border-color:#59486f}.dark .access-current{background:#173f3b}


.access-accounting{font-size:12px;color:#63728b;margin-top:-8px}.access-notice{padding:12px;border-radius:9px;color:#087d76;background:#e5faf5}.access-cancel{margin-top:10px;width:100%;padding:8px;text-decoration:underline}.access-page button:disabled{opacity:.5;cursor:not-allowed}.access-page button:focus-visible{outline:2px solid #14afa2;outline-offset:3px}.access-current span{font-size:12px}.access-row code{overflow-wrap:anywhere}.dark .access-current code{color:#c9f8ee}.dark .access-stat-good{background:#173f3b!important;border-color:#28695d!important}.dark .access-accounting{color:#bdb1cb}.access-token{max-width:620px}.access-token h2{font-size:18px;font-weight:700}.access-token input{font-family:inherit}
</style>
