# kerning

Search negotiated rates for a procedure in a health plan's price transparency file, and see where one provider sits
against the rest of the network.

It reads Centene's Transparency in Coverage files for Ambetter from Fidelis Care (NY individual market, 88 plans).

Built as a take-home: use a Transparency in Coverage index to find an in-network rate file, ingest it, and search it
by billing code with an optional NPI or EIN.

## Who it's for

- **A provider's contracting analyst** heading into a renegotiation: "For 45380, where does our EIN's rate sit against
  every other in-network provider?" Search a code, enter the EIN or NPI, and the ▲ marks it on each spread.
- **An employer or broker** deciding where to steer people: "Is a colonoscopy cheaper at a surgery center or a hospital,
  and who's cheap?" Search the code with no provider and compare settings.

This is individual-market data, so for an employer it shows the shape of the analysis, not rates they can buy.

## Run it

Needs Go 1.22+ and Node 20.19+ (Vite 8).

```sh
cd web && npm install && npm run build && cd ..
go run .
```

Open http://localhost:8080. The first start downloads the index and the 95 MB in-network file into `data/` (a few
seconds); later starts reuse them. Restart to pick up new files.

For UI work, run `go run .` and `cd web && npm run dev` side by side. Vite proxies `/api` to Go.

```sh
go test ./...
```

## How it works

```
index.json ──► in-network file ──► parse ──► build ──► Snapshot (in memory) ──► /api/rates, /api/procedures ──► Vue
```

1. **Download.** `main.go` takes an index URL, pinned to the 2026-09-28 index. The first reporting structure (the
   Ambetter exchange network) points to one in-network file. Both files are cached in `data/` by filename, and the
   filename carries the date.
2. **Parse** (`data.go`). `json.Decoder` streams the file one array element at a time. An element with the wrong shape
   is skipped and counted, and the rest of the file still loads.
3. **Build.** Join rates to providers, normalize, dedupe, and precompute every aggregate, so a search is only lookups,
   filtering and paging.
4. **Serve** (`api.go`). Two read-only endpoints over the snapshot. The Vue app keeps the whole search in the URL, so
   any view can be shared or bookmarked.

On load it logs what it read:

```
loaded …fidelis-ex_in-network.json: 1069 procedures, {Items:1554 SkippedItems:0 Rows:8286 Duplicates:187866 SkippedRows:0 BadNPIs:27}
```

### API

`GET /api/rates?code=45380` takes:

- `ein` or `npi`: narrows rows to one provider
- `class`: `professional` or `institutional`
- `pos`: place of service code
- `sort`: `rate`, `-rate` or `provider`
- `limit` (default 25, max 100) and `offset`

It returns the procedure, the network groups and totals, one page of rows, and the source file. Groups and totals
always describe the whole network, so a provider filter can still be compared against it.

`GET /api/procedures?q=colon` returns up to 10 codes by code prefix or name, most widely contracted first.

Errors are `{"error": "..."}` with a 400 (bad input) or 404 (no rates for that code).

## What the data looks like, and what I did about it

| | |
|---|---|
| In-network items | 1,554: 594 CPT, 81 HCPCS, 879 revenue codes (RC) |
| Provider references | 1,380 groups, ~41k NPIs, ~5.9k EINs |
| Price rows | 193k as published, 8.3k after dedupe (one per place of service) |
| Constant everywhere | fee for service, `negotiated`, outpatient, expires 9999-12-31 |

- **Most of the file is copies.** Revenue code items repeat one rate payload under many unrelated names: RC 490 appears
  166 times, byte for byte, labelled "INJECTION EYE DRUG", "REPAIR RADIUS OR ULNA" and so on. Identical rows are
  dropped (187,866 of them), and a revenue code only keeps its name when every item agrees on it.
- **Facility fees hide behind revenue codes.** On RC rows, the procedure is in `additional_information`: RC 490 (surgery
  center) with `45380` is the facility fee for a colonoscopy. Those rows are routed to the CPT, so 45380 shows both the
  professional (doctor's) fees from its own item and the facility fees, which a plain search on `billing_code` misses.
  RC rows with nothing in `additional_information` (lab, supplies, IV therapy) stay under the revenue code.
- **Providers come from `provider_references`.** Rates point to a `provider_group_id`; the NPIs and EINs live in the
  references. One reference can hold several EINs, and one NPI appears under up to 20 EINs. An NPI search matches every
  provider group the NPI belongs to.
- **NPI `0` is a placeholder.** It appears in every group of some references, and is dropped along with one 9-digit NPI.
- **Many CPTs are never named.** 262 CPTs appear only in RC `additional_information`, so the file has no description for
  them. The UI says so instead of guessing.

Every row keeps where it came from: file, billing code type and code, `additional_information`, `service_code` and
`provider_group_id`. Expand a row to see it.

## Decisions and tradeoffs

- **Professional and facility fees are never mixed**, and neither are places of service. A spread is one billing class
  at one place of service, like "facility fees at surgery centers". Mixing a $154 doctor's fee with an $829 facility fee
  gives a meaningless median.
- **A price listed for two places of service becomes two rows**, so each row sits in exactly one spread.
- **Each provider counts once per spread, at its base rate**: the row with the fewest modifiers, and the highest of
  those, since lower rates for the same code are often licensure tiers. All rows are
  still listed, and "Above" is the share of providers' base rates below that row.
- **Spreads are unweighted.** A 714-NPI group counts the same as a solo practice: this shows what contracts look like,
  not what an average visit costs. Weighting needs claims volume, which the file doesn't have.
- **Under 5 providers, there's no box plot.** The UI shows the individual rates instead.
- **Typical total** adds the professional and facility medians for a setting. The two fees go to different providers,
  so it's an estimate.
- **Everything is in memory.** After dedupe the file is small, so ingest runs at startup and search never touches disk.
- **Standard library only in Go, Vue + Vite only in the UI.**

## Limits

- One file: the exchange network. The index's second file (the Essential Plan, 6 plans) is a separate network, and
  loading it means picking the structure by plan.
- No zombie-rate filtering. Rates for codes a provider would never bill (a knee replacement under a counselor) need NPI
  taxonomy or claims data to catch.
- No Medicare benchmark, so a rate can't be shown as % of Medicare.
- Inline `provider_groups` on a rate, `bundled_codes` and `covered_services` aren't handled. This file doesn't use them.
- The index URL is pinned. Centene posts a new one monthly on
  https://www.centene.com/price-transparency-files.html.

## With more time

- Add the CMS physician and OPPS fee schedules and show every rate as % of Medicare.
- Load every file in the index, and compare one provider across networks and months.
- At full scale this becomes a batch pipeline that writes the precomputed groups to a columnar store, with the API
  reading aggregates instead of rows. The `Snapshot` boundary here is the small version of that split.
- Resolve the latest index from Centene's page instead of pinning it, and swap in a new snapshot without a restart.
- Flag likely mislabelled rates, like professional rates billed by a surgery center that look like facility fees.
- Licensed CPT descriptions for the codes the file never names.
