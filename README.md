# Wayfare

A corridor-integrity monitor for Stellar.

Wayfare prices a stablecoin → fiat-token corridor across trade sizes, scores
every route against an independent mid-market rate, and states plainly when
none of them are worth taking — including when the honest answer is *don't
send this*.

**Status:** pre-MVP, read-only. Non-custodial: no funds held, no tokens
issued, no KYC, no keys.

---

## Why a monitor and not a router

The project began as a router — find the cheapest path, rank the results. Live
mainnet measurement killed that thesis, and the measurement is the reason the
tool exists in its current form.

Sending 100 USDC to NGNC returns roughly 62,900 NGNC against a USD/NGN mid
near 1,364. The best available route delivers about 46% of fair value. A tool
that displayed *"best route: 62,900 NGNC"* would be accurate, useful-looking,
and would have cost the sender more than half of what they sent.

So a ranking is not enough. A ranking carries a hidden assumption — that its
winner is worth taking — and on this corridor that assumption is false at
every size tested. Every quote is scored against an independent reference
rate, and a corridor whose best option is unusable says so instead of
presenting a winner.

That is also why the reference rate is a required dependency rather than an
optional enrichment. Without it the engine can rank, but it cannot tell a good
deal from a disaster.

---

## What the measurements found

Full figures, with timestamps and raw output:
**[docs/corridor-measurements.md](docs/corridor-measurements.md)**

Measured live on 2026-08-08 against Horizon pathfinding, three corridors from
one issuer, all within a 60-second window:

| Corridor | Issuer status | Best result, any size | Mode |
|:---|:---|:---|:---|
| USDC → NGNC | `live` | 25.02% loss at 0.1 USDC, 97.68% at 5000 | Live, value-destroying |
| USDC → GHSC | `pending` | 74.14% loss at 0.1 USDC, 99.47% at 5000 | Derivative — every path runs through NGNC |
| USDC → KESC | `pending` | no route at any size | No market |

Three findings shaped the design:

**The loss has a structural floor.** At 0.1 USDC price impact is negligible,
and NGNC still loses 25%. That floor is the corridor's spread, not its depth —
which means no trade size can be acceptable, because the zero-size limit is
already unacceptable. Slippage then stacks on top, reaching 97.68% at 5000.

**The three corridors fail in three different ways.** One prices continuously
and prices badly. One has no independent market and inherits another token's
failure modes. One has no market at all. Reporting all three as "Unusable"
would be accurate and would discard the reason each is unusable — so the
monitor carries an integrity state alongside the loss grade.

**The benchmark is the charitable one.** The reference is the official
USD/NGN rate. If the rate people actually transact at is weaker, every figure
above *understates* the loss.

The issuer set is case study #1, not the product. Wayfare measures any
stablecoin → fiat-token corridor.

---

## Running it

```bash
make run                      # measure USDC -> NGNC against live mainnet
go run ./cmd/ladder -to GHSC  # any verified corridor
go run ./cmd/wayfared         # HTTP API + web UI on :8080
```

Go 1.22+. Dependencies: `shopspring/decimal` (money is never a float) and
`BurntSushi/toml`. Both binaries need live network access — there are no
cached figures to fall back on, by design.

### HTTP API

```
GET /api/corridor?to=NGNC[&from=USDC][&sizes=1,10,100]
GET /api/assets
GET /healthz
GET /                          single-file UI, no build step
```

`/api/corridor` returns the full size-ladder curve, the corridor's integrity
state, and a plain-language finding. Two fields carry the invariants:

- `recommended` is always present, and is `null` whenever no size produced an
  acceptable route. Clients must render null as "none" — never substitute the
  best-scoring quote.
- `integrity` is `DIRECT`, `DERIVATIVE`, or `NO-MARKET`. A client rendering
  only the loss curve will misreport the last two.

Money crosses the wire as decimal strings, so a client cannot parse a rate
into a float64.

---

## Design

```
asset/     Corridor endpoints, verified issuers, fiat-peg registry.
refrate/   Independent mid-market rate. Required — see above.
anchor/    SEP-1 discovery. Answers: can this anchor be priced at all?
sep38/     Anchor RFQ client.
dex/       On-chain pricing via Horizon pathfinding + market health.
route/     Ranks routes, scores against mid, issues verdict and integrity.
api/       HTTP surface and the embedded UI.
cmd/       ladder (measurement CLI), wayfared (server).
```

Four decisions worth knowing about:

**Integrity is separate from the verdict.** The verdict grades a loss
percentage, which only works when there is a price to grade. `NO-MARKET` has
no loss percentage at all; `DERIVATIVE` has one that means something different
because it compounds another corridor's. Detection reads a registry of tokens
whose peg was verified from the issuer's `stellar.toml`, so a hop through XLM
is a bridge asset while a hop through NGNC is a fiat dependency. The
derivative claim examines *every* path, since one path avoiding fiat
intermediaries would disprove it.

**SEP-38 fee denomination.** A fee may be denominated in either the sell or
the buy asset, and `fee.asset` says which. The naive `buy_amount + fee.total`
adds units of one currency to another — arithmetic that succeeds and produces
a meaningless number. Solving the spec's two price definitions for the pre-fee
gross gives one branch-free expression correct in both cases:

```
gross_in_buy_asset = sell_amount / price
```

Verified against the worked example in SEP-0038 itself: selling 542 BRL for
100 USDC at price 5.00 with a 42 BRL fee means the fee is **8.4 USDC**, not 42.

**Pathfinding, not order book walking.** Stellar settles path payments against
AMM liquidity pools *and* order book offers; the order book endpoint reports
only offers. Measured live, the order book could not reproduce Horizon's own
price for the same market at the same moment. Pricing delegates to
`/paths/strict-send` — the same engine that would execute the payment — and
the order book is used only as a market-health diagnostic.

**Asset code never identifies an asset.** Anyone can issue a token called
`USDC`. Every issuer here was read from the issuer's own `stellar.toml` per
SEP-1, with the verification date recorded, because anchors do rotate issuers.

---

## Non-goals

These keep the project shippable and legal for a small team:

- **Not an anchor.** Never issues tokens or holds reserves.
- **Not custodial.** Never takes possession of funds.
- **Not a money transmitter.** No custody, so no licensing surface.
- **Not a KYC provider.** Delegated to anchors via SEP-12.

---

## Verification status

| Claim | Status |
|---|---|
| NGNC / GHSC / KESC issuer `GASBV6W7…FQGXZY6` | Verified from ngnc.online stellar.toml, 2026-08-08 |
| GHSC and KESC are `status="pending"` | Verified from the same document |
| NGNC anchor lacks SEP-38 | Verified from live stellar.toml |
| Corridor figures in docs/ | Measured, live Horizon strict-send, timestamped |
| SEP-38 fee identity | Verified against SEP-0038 spec text |
| USDC issuer is Circle's | **Not yet verified** against circle.com stellar.toml |
| Live SEP-38 round-trip | **Not done** — no anchor on this corridor publishes a quote server |

Unverified claims are marked in the code at the point they are used.

Reference rates come from exchangerate-api's official/interbank figures. For
currencies under exchange controls the rate people actually transact at can
diverge, so this is a defensible benchmark rather than ground truth — and it
is the charitable direction for the corridors measured here.

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The invariants there are hard
constraints, not style preferences.

## License

Apache-2.0. See [LICENSE](LICENSE).
