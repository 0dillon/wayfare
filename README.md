# Wayfare

A non-custodial quote engine for the USDC → NGN remittance corridor on Stellar.

Wayfare prices every way money can travel a corridor — anchor RFQ quotes and
on-chain DEX paths — scores each against an independent mid-market rate, and
tells you the true all-in cost. Including when the honest answer is *don't
send this*.

**Status:** pre-MVP, read-only. No funds move. No custody, ever.

---

## What this found

The project began as a router: find the cheapest path, rank the results. Live
mainnet measurements on **2026-08-04** changed the design.

**1. The naira anchor cannot be priced by software.**

`ngnc.online` issues NGNC on mainnet and is the primary naira on/off-ramp. Its
`stellar.toml` publishes `WEB_AUTH_ENDPOINT` and `TRANSFER_SERVER_SEP0024` —
and no `ANCHOR_QUOTE_SERVER`. Under SEP-1 that field is how an anchor declares
SEP-38 support, so there is no machine-readable rate to fetch. Its price is
visible only inside a hosted flow, to a human, after authenticating.

**2. The on-chain route is not a market.**

The direct USDC/NGNC order book had 2 bid levels (one dust-priced at zero) and
5 ask levels (one at 6,000,000). Bid–ask spread: **128.8% of mid**.

**3. The best available route destroys more than half the money.**

Horizon's own pathfinder, asked to price 100 USDC → NGNC:

| Route | Receives | Implied rate |
|---|---:|---:|
| via XLM bridge | 65,100.14 NGNC | 651 NGN/USD |
| direct market | 21,785.78 NGNC | 218 NGN/USD |

Against a real USD/NGN near 1,500, the *winner* delivers ~43% of fair value.
A sender loses roughly **₦84,900 on a $100 transfer**.

Presented alone, "65,100 NGNC" looks like a fine number. It is only
recognisable as a disaster next to an outside reference rate — which is why
the reference rate is a required dependency of the engine, not an optional
enrichment, and why every quote carries a verdict rather than just a rank.

```
No viable route. The best of 1 priced route(s) still loses 56.6% against
the mid-market rate. Sending through this corridor at this size is not
recommended.
```

That output is the product working correctly.

---

## Design

```
asset/     Corridor endpoints. SEP-38 identification format, Horizon params.
refrate/   Independent mid-market rate. Required — see above.
anchor/    SEP-1 discovery. Answers: can this anchor be priced at all?
sep38/     Anchor RFQ client.
dex/       On-chain pricing via Horizon pathfinding + market health.
route/     Ranks routes, scores against mid, issues a verdict.
```

Two decisions worth knowing about:

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
price for the same market at the same moment. So pricing delegates to
`/paths/strict-send` — the same engine that would execute the payment — and
the order book is used only as a market-health diagnostic.

---

## Build

```bash
go build ./...
go test ./...
```

Go 1.22+. Dependencies: `shopspring/decimal` (money is never a float),
`BurntSushi/toml`.

---

## Non-goals

These keep the project shippable and legal for a solo maintainer:

- **Not an anchor.** Never issues tokens or holds reserves.
- **Not custodial.** Never takes possession of funds.
- **Not a money transmitter.** No custody, so no licensing surface.
- **Not a KYC provider.** Delegated to anchors via SEP-12.

---

## Verification status

| Claim | Status |
|---|---|
| NGNC issuer `GASBV6W7…FQGXZY6` | Verified from ngnc.online stellar.toml |
| NGNC anchor lacks SEP-38 | Verified from live stellar.toml |
| USDC/NGNC spread 128.8% | Measured, live Horizon order book |
| Best path 651 NGN/USD | Measured, live Horizon strict-send |
| SEP-38 fee identity | Verified against SEP-0038 spec text |
| USDC issuer is Circle's | **Not yet verified** against circle.com stellar.toml |
| Live SEP-38 round-trip | **Not done** — SDF testanchor returned 502 |

Unverified claims are marked in the code at the point they are used.

---

## License

MIT
