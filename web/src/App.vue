<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import RangeBar from './RangeBar.vue'

const classes = { professional: 'Professional', institutional: 'Facility' }
const usd = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' })
const money = (n) => usd.format(n)

const form = reactive({ code: '', provider: '' })
const filters = reactive({ class: '', pos: '', sort: 'rate', offset: 0 })
const suggestions = ref([])
const result = ref(null)
const error = ref('')
const loading = ref(false)
const open = ref(null)

const procedure = computed(() => result.value?.procedure)
const groups = computed(() => result.value?.groups ?? [])
const scale = computed(() => Math.max(...groups.value.map((g) => g.max)))
const labels = computed(() => Object.fromEntries(groups.value.map((g) => [g.key, `${classes[g.class]} · ${g.pos_label}`])))
const settings = computed(() => [...new Map(groups.value.map((g) => [g.pos, g.pos_label]))])
const lensName = computed(() => result.value?.lens?.matched.map((e) => e.name).join(', '))

function params() {
  const p = new URLSearchParams({ code: form.code })
  const id = form.provider.replace(/\D/g, '')
  if (id.length === 10) p.set('npi', id)
  else if (id) p.set('ein', id)
  for (const [k, v] of Object.entries(filters)) {
    if (v && !(k === 'sort' && v === 'rate')) p.set(k, v)
  }
  return p
}

function readURL() {
  const p = new URLSearchParams(location.search)
  form.code = p.get('code') ?? ''
  form.provider = p.get('ein') ?? p.get('npi') ?? ''
  filters.class = p.get('class') ?? ''
  filters.pos = p.get('pos') ?? ''
  filters.sort = p.get('sort') ?? 'rate'
  filters.offset = Number(p.get('offset') ?? 0)
}

async function load() {
  if (!form.code) return
  loading.value = true
  error.value = ''
  open.value = null
  try {
    const res = await fetch('/api/rates?' + params())
    const body = await res.json()
    if (!res.ok) throw new Error(body.error)
    result.value = body
  } catch (e) {
    error.value = e.message
    result.value = null
  } finally {
    loading.value = false
  }
}

function go(changes) {
  Object.assign(filters, changes)
  history.pushState(null, '', '?' + params())
  load()
}

function search() {
  const id = form.provider.replace(/\D/g, '')
  if (id && id.length !== 9 && id.length !== 10) {
    error.value = 'Provider must be a 9-digit EIN or a 10-digit NPI'
    return
  }
  // A name typed without picking a suggestion searches the top suggestion.
  if (!/\d/.test(form.code)) {
    if (!suggestions.value.length) {
      error.value = `No procedure name matches "${form.code}". Many codes have no name in the source file, so try the code.`
      return
    }
    form.code = suggestions.value[0].code
  }
  go({ class: '', pos: '', offset: 0 })
}

async function suggest() {
  const q = form.code
  if (q.length < 2) return
  const res = await fetch('/api/procedures?q=' + encodeURIComponent(q))
  // Typing fast can return answers out of order; keep only the latest.
  if (res.ok && q === form.code) suggestions.value = await res.json()
}

function npiCount(provider) {
  return provider.eins.reduce((n, e) => n + (e.npis?.length ?? 0), 0)
}

onMounted(() => {
  readURL()
  load()
  window.addEventListener('popstate', () => {
    readURL()
    load()
  })
})
</script>

<template>
  <header>
    <h1>kerning</h1>
    <p>Negotiated rates for Ambetter from Fidelis Care (NY), from Centene's price transparency files.</p>
  </header>

  <form @submit.prevent="search">
    <label>
      Procedure
      <input v-model.trim="form.code" list="procedures" placeholder="Code or name, e.g. 45380 or colonoscopy" required @input="suggest">
    </label>
    <datalist id="procedures">
      <option v-for="p in suggestions" :key="p.code" :value="p.code">{{ p.description || p.name || 'No description in source file' }}</option>
    </datalist>
    <label>
      Provider
      <input v-model.trim="form.provider" placeholder="Optional EIN or NPI">
    </label>
    <button :disabled="loading">Search</button>
  </form>

  <p v-if="error" class="error">{{ error }}</p>
  <p v-else-if="loading && !result" class="muted">Loading…</p>

  <main v-if="result" :class="{ loading }">
    <section>
      <h2>{{ procedure.code }} <span v-if="procedure.name">{{ procedure.name }}</span><span v-else class="muted">No description in source file</span></h2>
      <p v-if="procedure.description" class="muted">{{ procedure.description }}</p>
      <p>{{ result.network_total }} rates from {{ procedure.providers }} providers</p>
      <p v-if="result.lens && !result.lens.matched.length" class="error">No provider with that {{ result.lens.ein ? 'EIN' : 'NPI' }} is in this network.</p>
      <p v-else-if="result.lens">Showing <strong>{{ lensName }}</strong> against the network (▲).</p>
    </section>

    <section>
      <h3>Rates by setting</h3>
      <p class="muted">Each provider counts once, at its base rate: the fewest modifiers, and the highest of those.</p>
      <div v-for="g in groups" :key="g.key" class="group">
        <div class="group-label">
          {{ classes[g.class] }} · {{ g.pos_label }}
          <span class="muted">{{ g.values.length }} {{ g.values.length === 1 ? 'provider' : 'providers' }}</span>
        </div>
        <RangeBar :group="g" :scale="scale" />
        <div class="muted">
          <template v-if="g.values.length >= 5">median {{ money(g.median) }} · {{ money(g.min) }} to {{ money(g.max) }}</template>
          <template v-else>{{ g.values.map(money).join(', ') }}</template>
          <span v-for="m in g.lens_marks" :key="m.rate" class="lens"> · ▲ {{ money(m.rate) }}, above {{ m.percentile }}% of providers</span>
        </div>
      </div>
    </section>

    <section v-if="result.totals.length">
      <h3>Typical total</h3>
      <p class="muted">Professional plus facility medians. The two fees are paid to different providers, so this is an estimate, not a quote.</p>
      <div class="totals">
        <div v-for="t in result.totals" :key="t.pos" class="total">
          <div>{{ t.pos_label }}</div>
          <strong>{{ money(t.estimate) }}</strong>
          <div class="muted">{{ money(t.professional) }} + {{ money(t.institutional) }}</div>
        </div>
      </div>
    </section>

    <section>
      <h3>Rates</h3>
      <div class="filters">
        <select :value="filters.class" @change="go({ class: $event.target.value, offset: 0 })">
          <option value="">All fee types</option>
          <option value="professional">Professional</option>
          <option value="institutional">Facility</option>
        </select>
        <select :value="filters.pos" @change="go({ pos: $event.target.value, offset: 0 })">
          <option value="">All settings</option>
          <option v-for="[pos, label] in settings" :key="pos" :value="pos">{{ label }}</option>
        </select>
        <select :value="filters.sort" @change="go({ sort: $event.target.value, offset: 0 })">
          <option value="rate">Lowest rate</option>
          <option value="-rate">Highest rate</option>
          <option value="provider">Provider name</option>
        </select>
      </div>

      <p v-if="!result.rows.length" class="muted">No rates match.</p>
      <table v-else>
        <thead>
          <tr><th>Provider</th><th>Setting</th><th>Rate</th><th>Above</th><th>NPIs</th></tr>
        </thead>
        <tbody>
          <template v-for="(r, i) in result.rows" :key="i">
            <tr class="row" @click="open = open === i ? null : i">
              <td data-label="Provider">
                {{ r.provider.eins[0].name }}
                <span v-if="r.provider.eins.length > 1" class="muted">+{{ r.provider.eins.length - 1 }} more</span>
                <small>EIN {{ r.provider.eins[0].ein }}</small>
              </td>
              <td data-label="Setting">{{ labels[r.group] }}</td>
              <td data-label="Rate">
                {{ money(r.rate) }}
                <small v-if="r.modifiers?.length">modifier {{ r.modifiers.join(', ') }}</small>
              </td>
              <td data-label="Above">{{ r.percentile }}%<small v-if="!r.base">not base rate</small></td>
              <td data-label="NPIs">{{ npiCount(r.provider) }}</td>
            </tr>
            <tr v-if="open === i" class="detail">
              <td colspan="5">
                <p v-for="e in r.provider.eins" :key="e.ein">
                  <strong>{{ e.name }}</strong> · EIN {{ e.ein }}<br>
                  <span class="muted">NPIs {{ e.npis?.slice(0, 12).join(', ') }}<template v-if="e.npis?.length > 12"> and {{ e.npis.length - 12 }} more</template></span>
                </p>
                <p class="muted">
                  From {{ result.source.file }}: {{ r.source.billing_code_type }} {{ r.source.billing_code }}
                  <template v-if="r.source.additional_information">, additional_information {{ r.source.additional_information }}</template>,
                  service_code {{ r.source.service_code.join(', ') }}, provider_group_id {{ r.provider.ref }}
                </p>
              </td>
            </tr>
          </template>
        </tbody>
      </table>

      <div v-if="result.total > result.limit" class="pager">
        <button :disabled="result.offset === 0" @click="go({ offset: Math.max(0, result.offset - result.limit) })">Previous</button>
        <span>{{ result.offset + 1 }}–{{ Math.min(result.offset + result.limit, result.total) }} of {{ result.total }}</span>
        <button :disabled="result.offset + result.limit >= result.total" @click="go({ offset: result.offset + result.limit })">Next</button>
      </div>
    </section>
  </main>

  <footer v-if="result">
    <a :href="result.source.index">{{ result.source.file }}</a> · updated {{ result.source.updated }} · covers {{ result.source.plans }} plans
  </footer>
</template>
