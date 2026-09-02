# hofors.se — decision extraction notes

- This chain targets Hofors kommun (Kommunfullmäktige). Verify that the
  source protocol actually belongs to Hofors before extracting.
- Contamination sweep: a protocol that names another municipality (e.g.
  Huddinge kommun) anywhere in the text is not this chain's document. Reject
  with `rejected: true` and a rejection_reason instead of extracting
  decisions, even if the meeting type (Kommunfullmäktige) and date match.
  Do not re-derive the persisted organization name into the result set.
