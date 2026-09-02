# askersund.se scanner notes (Askersund kommun)

## Cross-municipality documents
- The contamination sweep on re-vet flags submissions whose organization_name
  does not match the chain target (Askersund) and instructs re-deriving the
  whole meeting (metadata, decisions, sub-entities) from the document text,
  which is authoritative.
- When the document is genuinely from another municipality, keep the
  organization fields as written in the document (name/level/location from the
  document) instead of rewriting them to the chain target. Do not reject a
  Kommunfullmäktige document solely because its municipality differs from the
  chain target; meeting type is the rejection criterion.
- Party abbreviations in such protocols (S, C, V, SD, L, M) are already covered
  by canonicalization.md mappings to full party names.
