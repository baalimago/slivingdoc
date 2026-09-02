# Berg KF decision extraction guidance

- Bergs kommun protocols reach this chain even when the retrieval chain name
  differs; the protocol text (place, berg.se, organization name) is authoritative
  for organization_name. Verify against the document before trusting a
  contamination-sweep rejection that names another municipality.
- Other municipalities' KF protocols also reach this chain (observed: Arjeplogs
  kommun 2026-06-22). The document text is authoritative: derive
  organization_name / organization_level / organization_location from the
  protocol itself, never from the chain target, and do NOT reject solely
  because the municipality differs from Berg (same rule as boden.se note).
  Reject only when the meeting type does not match the expected type.
- Paragraphs are numbered "KF § N" in the source; keep that prefix in
  paragraph_number.
- Decisions appear under a "Kommunfullmäktiges beslut" heading; extract each
  paragraph with a substantive outcome (godkänna, fastställa, bifalla, remittera,
  bevilja, lägga till handlingarna, anta, besluta, välja, befria).
- Informational sections (rapport från kommunledningen, information från
  revisorerna, fråga till Allmänhetens frågestund) carry no beslut; skip them.
- Jäv declarations appear under a "Jäv" heading inside decision paragraphs and
  are also marked per-politician on the närvaro/voteringslista; keep them in the
  decision full_text (no separate protocol_notes section exists in this format).
- No counted omröstningar appear in this protocol type; omit voting_method,
  votes, aggregate_votes, proposals and reservations unless recorded.
- Ärendelista numbers the sammanträdespunkter separately from the KF § numbers;
  map by title/paragraph, not by list number.
