# strangnas.se chain — decision extraction guidance

- A scanned protocol in this chain may come from a different municipality
  than the chain target (strangnas.se). The protocol text is authoritative:
  set organization_name / organization_level / organization_location from the
  document itself, never from the chain target or from persisted values of an
  earlier run. Do not reject solely because the municipality differs; reject
  only when the meeting type does not match the expected type
  (Kommunfullmäktige).
- On re-vet ("contamination sweep") runs, re-derive all metadata, decisions and
  sub-entities (proposals, votes, aggregate votes, reservations, protocol
  notes) from the document instead of trusting persisted values — persisted
  sub-entity counts can be empty even when the source records them.
- Jäv notes ("deltar inte i ärendets handläggning och beslut på grund av jäv")
  are recorded per decision as protocol_notes tied to the named politicians.
- Yrkanden recorded as "X (parti), biträdd av Y (parti), yrkar bifall till
  kommunstyrelsens förslag" map to one proposal whose proposers are X and Y.
