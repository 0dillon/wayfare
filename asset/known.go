package asset

// Verified mainnet issuers.
//
// Every issuer here was read from the issuer's own published stellar.toml
// (SEP-1) rather than copied from a block explorer or a blog post. Asset code
// alone is never sufficient identification: anyone can issue a token called
// "USDC", and a router that matched on code alone would happily quote a
// worthless lookalike. The verification date is recorded because anchors do
// rotate issuers.
//
// Verification status, 2026-08-08, read from
// https://ngnc.online/.well-known/stellar.toml:
//
//   - NGNC   VERIFIED, status="live". Issued by LINK.IO LTD., pegged 1:1 to
//     NGN, anchor_asset_type="fiat". NETWORK_PASSPHRASE = public mainnet.
//
//   - GHSC   VERIFIED as published, status="pending". Same issuing account as
//     NGNC. The anchor itself does not declare this asset in service.
//
//   - KESC   VERIFIED as published, status="pending". Same issuing account.
//     Note the entry sets anchor_asset="KESC", naming its own token rather
//     than the ISO-4217 code KES that SEP-1 intends. Read as KES.
//
//   - USDC   NOT YET VERIFIED against circle.com's stellar.toml. This is the
//     widely-published Circle issuer and Horizon accepted it for live
//     orderbook and path queries, which proves it is a real, actively traded
//     issuer — but not that it is Circle's. Confirm before any mainnet
//     execution path ships. See VerifyAgainstTOML in package anchor.
//
// The pending status on GHSC and KESC is a first-class finding, not a detail
// to route around. Per SEP-1 only "live" means in service, and the monitor
// reports an asset its own issuer has not launched as exactly that rather
// than pricing it as though it were tradeable.
const (
	// USDCIssuer is Circle's mainnet USDC issuing account.
	USDCIssuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

	// LinkIOIssuer issues NGNC, GHSC and KESC from one account. It is the
	// single point of failure behind this issuer's entire African-fiat set.
	LinkIOIssuer = "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"

	// NGNCIssuer is retained as the original name for LinkIOIssuer.
	NGNCIssuer = LinkIOIssuer
)

// USDC is the settlement asset senders start from.
func USDC() Asset { return Stellar("USDC", USDCIssuer) }

// NGNC is the naira-denominated token that terminates the on-chain leg.
// Declared live by its issuer.
func NGNC() Asset { return Stellar("NGNC", LinkIOIssuer) }

// GHSC is the Ghanaian cedi token from the same issuer as NGNC. Its issuer
// declares it status="pending" — not in service.
func GHSC() Asset { return Stellar("GHSC", LinkIOIssuer) }

// KESC is the Kenyan shilling token from the same issuer as NGNC. Its issuer
// declares it status="pending" — not in service.
func KESC() Asset { return Stellar("KESC", LinkIOIssuer) }

// NGN is off-chain naira — what actually lands in a recipient's bank account.
func NGN() Asset { return Fiat("NGN") }

// GHS is off-chain Ghanaian cedi.
func GHS() Asset { return Fiat("GHS") }

// KES is off-chain Kenyan shilling.
func KES() Asset { return Fiat("KES") }
