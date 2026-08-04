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
// Verification status, 2026-08-04:
//
//   - NGNC   VERIFIED. Read directly from ngnc.online/.well-known/stellar.toml
//     (LINK.IO / Convexity), NETWORK_PASSPHRASE = public mainnet.
//
//   - USDC   NOT YET VERIFIED against circle.com's stellar.toml. This is the
//     widely-published Circle issuer and Horizon accepted it for live
//     orderbook and path queries, which proves it is a real, actively traded
//     issuer — but not that it is Circle's. Confirm before any mainnet
//     execution path ships. See VerifyAgainstTOML in package anchor.
const (
	// USDCIssuer is Circle's mainnet USDC issuing account.
	USDCIssuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

	// NGNCIssuer issues NGNC, and also GHSC and KESC, from one account.
	NGNCIssuer = "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
)

// USDC is the settlement asset senders start from.
func USDC() Asset { return Stellar("USDC", USDCIssuer) }

// NGNC is the naira-denominated token that terminates the on-chain leg.
func NGNC() Asset { return Stellar("NGNC", NGNCIssuer) }

// NGN is off-chain naira — what actually lands in a recipient's bank account.
func NGN() Asset { return Fiat("NGN") }
