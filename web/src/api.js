// The Go server answers /api/* itself. The static build (GitHub Pages) has no
// server, so it reads the files written by `go run . -export` and does the same
// filtering, sorting and paging here. Keep this in step with api.go.
const isStatic = import.meta.env.VITE_STATIC === '1'

async function getJSON(url) {
  const res = await fetch(url)
  const body = await res.json().catch(() => ({ error: `${url}: ${res.status} ${res.statusText}` }))
  if (!res.ok) throw new Error(body.error)
  return body
}

export async function getProcedures(q) {
  if (!isStatic) return getJSON('/api/procedures?q=' + encodeURIComponent(q))

  q = q.trim().toLowerCase()
  const all = await getJSON('data/procedures.json')
  return all
    .filter((p) => p.code.toLowerCase().startsWith(q) || p.name.toLowerCase().includes(q) || p.description.toLowerCase().includes(q))
    .slice(0, 10)
}

export async function getRates(params) {
  if (!isStatic) return getJSON('/api/rates?' + params)

  const code = (params.get('code') ?? '').trim().toUpperCase()
  if (!code) throw new Error('code is required')
  const limit = Number(params.get('limit') ?? 25)
  const offset = Number(params.get('offset') ?? 0)
  if (!Number.isInteger(limit) || limit < 1 || limit > 100) throw new Error('limit must be between 1 and 100')
  if (!Number.isInteger(offset) || offset < 0) throw new Error('offset must be 0 or more')
  const cls = params.get('class') ?? ''
  const pos = params.get('pos') ?? ''
  if (cls && cls !== 'professional' && cls !== 'institutional') throw new Error('class must be professional or institutional')
  const sort = params.get('sort') ?? 'rate'
  if (!['rate', '-rate', 'provider'].includes(sort)) throw new Error('sort must be rate, -rate or provider')

  const lens = await lensFor(params.get('ein'), params.get('npi'))
  let res
  try {
    res = await getJSON(`data/rates/${encodeURIComponent(code)}.json`)
  } catch {
    throw new Error('no in-network rates for code ' + code)
  }

  const groups = res.groups.map((g) => ({ ...g, lens_marks: [] }))
  const rows = []
  for (const row of res.rows) {
    if (lens && !lens.has(row.provider)) continue
    if (lens && row.base) {
      groups.find((g) => g.key === row.group).lens_marks.push({ rate: row.rate, percentile: row.percentile })
    }
    if ((cls && row.class !== cls) || (pos && row.pos !== pos)) continue
    rows.push(row)
  }

  // Rows come sorted by rate with tie-breaks, and sort() is stable, so pages stay consistent.
  const name = (r) => r.provider.eins[0].name
  if (sort === '-rate') rows.sort((a, b) => b.rate - a.rate)
  if (sort === 'provider') rows.sort((a, b) => (name(a) < name(b) ? -1 : name(a) > name(b) ? 1 : 0))

  const start = Math.min(offset, rows.length)
  return {
    ...res,
    lens: lens?.info ?? null,
    groups,
    rows: rows.slice(start, start + limit),
    total: rows.length,
    limit,
    offset: start,
  }
}

async function lensFor(ein, npi) {
  if (!ein && !npi) return null
  if (ein && npi) throw new Error('use ein or npi, not both')
  const network = await getJSON('data/network.json')

  if (ein) {
    const digits = ein.replaceAll('-', '')
    if (!/^\d{9}$/.test(digits)) throw new Error('ein must be 9 digits, like 12-3456789')
    ein = `${digits.slice(0, 2)}-${digits.slice(2)}`
    const matched = network.eins[ein] ? [{ ein, name: network.eins[ein] }] : []
    return { info: { ein, matched }, has: (p) => p.eins.some((e) => e.ein === ein) }
  }

  if (!/^\d{10}$/.test(npi)) throw new Error('npi must be 10 digits')
  const matched = (network.npis[npi] ?? []).map((e) => ({ ein: e, name: network.eins[e] }))
  return { info: { npi, matched }, has: (p) => p.eins.some((e) => e.npis.includes(Number(npi))) }
}
