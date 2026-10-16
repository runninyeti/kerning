<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import RangeBar from './RangeBar.vue'

const classes = { professional: 'Professional', institutional: 'Facility' }
const examples = [
  { code: '45380', provider: '', label: 'Colonoscopy with biopsy' },
  { code: '99213', provider: '', label: 'Office visit' },
  { code: '73610', provider: '', label: 'Ankle X-ray' },
  { code: '45380', provider: '46-0857425', label: 'One surgery center against the network' },
]
const usd = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' })
const money = (n) => usd.format(n)

const form = reactive({ code: '', provider: '' })
const filters = reactive({ class: '', pos: '', sort: 'rate', offset: 0 })
const suggestions = ref([])
const result = ref(null)
const error = ref('')
const loading = ref(false)
const open = ref(null)
const dark = ref(matchMedia('(prefers-color-scheme: dark)').matches)

const procedure = computed(() => result.value?.procedure)
const groups = computed(() => result.value?.groups ?? [])
const scale = computed(() => Math.max(...groups.value.map((g) => g.max)))
const labels = computed(() => Object.fromEntries(groups.value.map((g) => [g.key, `${classes[g.class]} · ${g.pos_label}`])))
const settings = computed(() => [...new Map(groups.value.map((g) => [g.pos, g.pos_label]))])
const lensName = computed(() => result.value?.lens?.matched.map((e) => e.name).join(', '))
const lensHasRates = computed(() => groups.value.some((g) => g.lens_marks?.length))

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

// Picking from the suggestion list searches right away; typing only suggests.
function onCodeInput(e) {
  const picked = !e.inputType || e.inputType === 'insertReplacementText'
  if (picked && suggestions.value.some((s) => s.code === e.target.value.trim())) {
    form.code = e.target.value.trim()
    search()
  }
  else suggest()
}

async function suggest() {
  const q = form.code
  if (q.length < 2) return
  const res = await fetch('/api/procedures?q=' + encodeURIComponent(q))
  // Typing fast can return answers out of order; keep only the latest.
  if (res.ok && q === form.code) suggestions.value = await res.json()
}

function toggle(i) {
  open.value = open.value === i ? null : i
}

// With a provider lens, show the business that matched rather than the group's first one.
function shown(provider) {
  const matched = result.value.lens?.matched.map((e) => e.ein) ?? []
  return provider.eins.find((e) => matched.includes(e.ein)) ?? provider.eins[0]
}

function plural(n) {
  return n === 1 ? '1 provider' : `${n} providers`
}

function providers(cls, pos) {
  return plural(groups.value.find((g) => g.key === `${cls}:${pos}`).values.length)
}

function tryExample(e) {
  form.code = e.code
  form.provider = e.provider
  search()
}

function npiCount(provider) {
  return provider.eins.reduce((n, e) => n + (e.npis?.length ?? 0), 0)
}

function setTheme(isDark) {
  dark.value = isDark
  document.documentElement.style.colorScheme = isDark ? 'dark' : 'light'
}

function toggleTheme() {
  setTheme(!dark.value)
  try {
    localStorage.theme = dark.value ? 'dark' : 'light'
  } catch {}
}

onMounted(() => {
  try {
    if (localStorage.theme) setTheme(localStorage.theme === 'dark')
  } catch {}
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
    <button type="button" class="theme" @click="toggleTheme">{{ dark ? 'Light' : 'Dark' }} mode</button>
    <p>Negotiated rates for Ambetter from Fidelis Care (NY), from Centene's price transparency files.</p>
  </header>

  <form @submit.prevent="search">
    <label>
      Procedure
      <input v-model.trim="form.code" list="procedures" placeholder="Code or name, e.g. 45380 or colonoscopy" required @input="onCodeInput">
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

  <section v-if="!result && !loading" class="intro">
    <p>Search a billing code to see what every in-network provider is paid for it, split by fee type and setting. Add an EIN or NPI to see where one provider sits against the rest.</p>
    <div class="examples">
      <button v-for="e in examples" :key="e.label" type="button" @click="tryExample(e)">
        <strong>{{ e.code }}</strong> {{ e.label }}
      </button>
    </div>
  </section>

  <main v-if="result" :class="{ loading }">
    <section>
      <h2>{{ procedure.code }} <span v-if="procedure.name" class="caps">{{ procedure.name }}</span><span v-else class="muted">No description in source file</span></h2>
      <p v-if="procedure.description" class="muted caps">{{ procedure.description }}</p>
      <dl class="stats">
        <div><dt>Rates</dt><dd>{{ result.network_total }}</dd></div>
        <div><dt>Providers</dt><dd>{{ procedure.providers }}</dd></div>
        <div><dt>Settings</dt><dd>{{ groups.length }}</dd></div>
      </dl>
      <p v-if="result.lens && !result.lens.matched.length" class="error">No provider with that {{ result.lens.ein ? 'EIN' : 'NPI' }} is in this network.</p>
      <p v-else-if="result.lens && !lensHasRates" class="error">{{ lensName }} is in this network but has no rate for {{ procedure.code }}.</p>
      <p v-else-if="result.lens">Showing <strong>{{ lensName }}</strong> against the network (▲).</p>
    </section>

    <section>
      <h3>Rates by setting</h3>
      <p class="muted">Each provider counts once, at its base rate: the fewest modifiers, and the highest of those.</p>
      <div class="spread spread-head muted">
        <span>Setting</span>
        <span class="axis"><span>$0</span><span>{{ money(scale) }}</span></span>
        <span class="num">Median</span>
      </div>
      <div v-for="g in groups" :key="g.key" class="spread">
        <div>{{ classes[g.class] }} · {{ g.pos_label }}<small>{{ plural(g.values.length) }}</small></div>
        <RangeBar :group="g" :scale="scale" />
        <div class="num">
          {{ money(g.median) }}
          <small>{{ g.values.length >= 5 ? `${money(g.min)} – ${money(g.max)}` : g.values.map(money).join(', ') }}</small>
        </div>
        <div v-for="m in g.lens_marks" :key="m.rate" class="lens">▲ {{ money(m.rate) }}, above {{ m.percentile }}% of providers</div>
      </div>
    </section>

    <section v-if="result.totals.length">
      <h3>Typical total</h3>
      <p class="muted">Professional plus facility medians. The two fees are paid to different providers, so this is an estimate, not a quote.</p>
      <div class="totals">
        <div v-for="t in result.totals" :key="t.pos" class="total">
          <div>{{ t.pos_label }}</div>
          <strong>{{ money(t.estimate) }}</strong>
          <div class="muted">{{ money(t.professional) }} ({{ providers('professional', t.pos) }}) + {{ money(t.institutional) }} ({{ providers('institutional', t.pos) }})</div>
        </div>
      </div>
    </section>

    <section>
      <h3>Rates</h3>
      <div class="filters">
        <select aria-label="Fee type" :value="filters.class" @change="go({ class: $event.target.value, offset: 0 })">
          <option value="">All fee types</option>
          <option value="professional">Professional</option>
          <option value="institutional">Facility</option>
        </select>
        <select aria-label="Setting" :value="filters.pos" @change="go({ pos: $event.target.value, offset: 0 })">
          <option value="">All settings</option>
          <option v-for="[pos, label] in settings" :key="pos" :value="pos">{{ label }}</option>
        </select>
        <select aria-label="Sort" :value="filters.sort" @change="go({ sort: $event.target.value, offset: 0 })">
          <option value="rate">Lowest rate</option>
          <option value="-rate">Highest rate</option>
          <option value="provider">Provider name</option>
        </select>
      </div>

      <p v-if="!result.rows.length" class="muted">No rates match.</p>
      <table v-else>
        <thead>
          <tr><th>Provider</th><th>Setting</th><th class="num">Rate</th><th class="num">Above</th><th class="num">NPIs</th></tr>
        </thead>
        <tbody>
          <template v-for="(r, i) in result.rows" :key="i">
            <tr class="row" tabindex="0" :aria-expanded="open === i" @click="toggle(i)" @keydown.enter="toggle(i)" @keydown.space.prevent="toggle(i)">
              <td data-label="Provider">
                {{ shown(r.provider).name }}
                <span v-if="r.provider.eins.length > 1" class="muted">+{{ r.provider.eins.length - 1 }} more</span>
                <small>EIN {{ shown(r.provider).ein }}</small>
              </td>
              <td data-label="Setting">{{ labels[r.group] }}</td>
              <td data-label="Rate" class="num">
                {{ money(r.rate) }}
                <small v-if="r.modifiers?.length">modifier {{ r.modifiers.join(', ') }}</small>
              </td>
              <td data-label="Above" class="num">{{ r.percentile }}%<small v-if="!r.base">not base rate</small></td>
              <td data-label="NPIs" class="num">{{ npiCount(r.provider) }}</td>
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
        <span>{{ result.rows.length ? `${result.offset + 1}–${result.offset + result.rows.length}` : 0 }} of {{ result.total }}</span>
        <button :disabled="result.offset + result.limit >= result.total" @click="go({ offset: result.offset + result.limit })">Next</button>
      </div>
    </section>
  </main>

  <footer v-if="result">
    <a :href="result.source.index">{{ result.source.file }}</a> · updated {{ result.source.updated }} · covers {{ result.source.plans }} plans
  </footer>
</template>
