# Bjuvs kommun KF – decision extraction

- This chain is scoped to Bjuvs kommun KF, but the retrieval bucket may
  contain protocols from other municipalities (observed: an Askersunds kommun
  KF meeting persisted under this chain). The protocol text is authoritative
  for organization_name (Bjuvs kommun) and all meeting facts; if a persisted
  submission names another municipality, re-derive the whole meeting
  (metadata, decisions, sub-entities) from the document.
- Protocols: SAMMANTRÄDESPROTOKOLL for Kommunfullmäktige; agenda items are
  numbered §-blocks with an ärendelista up front; each item ends with a
  "Kommunfullmäktiges beslut" block that is the decision to extract.
- Party abbreviations are standard (SD, M, S, V, C, KD); no-party members are
  rendered "(-)" in attendance and vote lists — canonical form "--"
  (see canonicalization.md).
- Voteringar ("Votering begärs och ska genomföras") are followed by
  "Omröstningsresultat" totals and, in the appended bilaga, a per-person
  vote list (Ja/Nej/Avstår per name and party); extract both the individual
  votes and the aggregate result. If the voteringslista bilaga is not part of
  the document text, only the totals are available — record them in the
  decision summary and omit votes/aggregate_votes (no party breakdown).
- Attendance lists are authoritative for politicians; the elected
  officers (ordförande / vice ordförande) are named in the list;
  tjänstgörande ersättare appear as "X för Y", giving identifiable_tags for
  who they substitute for.
- Information items (e.g. "Information om ..." ending in a "tacka för
  informationen" decision) are not substantive decisions; skip them.
  Justeringsanteckningar attached to a § (a justerare contesting a decision)
  are protocol_notes. A roll-call/upprop paragraph that carries its own
  "Kommunfullmäktiges beslut" block (e.g. fastställande av antal
  tjänstgörande ledamöter) records an explicit decision outcome and can be
  extracted like any other beslut block.
- Redovisning items (motioner / medborgarförslag som ännu ej avslutats) that
  close with "Kommunfullmäktige beslutar att godkänna redovisningen" are formal
  approval decisions - extract them. "Anmälningar" items closing with "att
  anmälningen ska anses vara anmäld" are announcements - skip them (same as
  info items ending in "tacka för informationen").
