# Decision extraction guidance (ljungby.se chain)

- A scanned protocol in this chain may explicitly name a different municipality
  than the chain target (ljungby.se). The protocol text is authoritative:
  set organization_name / organization_location from the text itself, never
  from the chain target or from persisted values of another run.
- On re-vet ("contamination sweep") runs, re-derive all metadata, decisions and
  sub-entities (proposals, votes, aggregate votes, reservations, protocol notes)
  from the document instead of trusting persisted values — persisted sub-entity
  counts can be empty even when the source records full votering bilagor.
