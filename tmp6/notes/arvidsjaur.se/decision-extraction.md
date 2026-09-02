# arvidsjaur.se scanner notes (Arvidsjaur kommun)

## Cross-municipality contamination
- The arvidsjaur.se chain can receive Kommunfullmäktige protocols from other
  municipalities; these must be rejected (rejected: true + rejection_reason),
  not extracted.
- Aneby kommun KF protocols are recognizable without a municipality header:
  content markers include Askeryd socken, Vireda skola, Lönhult,
  Sommenbygdens folkhögskola, and Höglandsförbundet (Aneby, Eksjö, Sävsjö,
  Nässjö, Vetlanda). Aneby KF protocols belong to the aneby.se chain
  (see aneby.se/README.md for its harvest list, which includes 2022-03-28).
- Always verify organization/chain fit from internal document evidence before
  extracting; the meeting type alone (Kommunfullmäktige) does not guarantee
  the document belongs to this chain.
- Contamination sweeps may flag Arvidsjaur KF protocols citing a different
  target municipality (e.g. "Askersund"). This chain's target is Arvidsjaur:
  genuine Arvidsjaur protocols carry their own header (ARVIDSJAURS KOMMUN /
  Árviesjávrien kommuvdna) plus Arvidsjaur-specific markers (Arvidsjaur
  Flygplats AB, sport- och simhall, K4, Norrbotten references). When the
  document shows these, extract with organization_name "Arvidsjaurs kommun";
  do not reject on the sweep message alone.
