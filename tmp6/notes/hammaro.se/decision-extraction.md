# Decision extraction guidance (hammaro.se chain)

- Chain target is Hammarö kommun (Kommunfullmäktige). A scanned protocol in
  this chain may explicitly name a different municipality (e.g. a Halmstads
  kommun KF protocol landed here on 2026-06-16). Follow the ljungby.se rule:
  the protocol text is authoritative — set organization_name /
  organization_location from the text itself, never from the chain target or
  from persisted values of another run; extract normally, do not reject, as
  long as the meeting type (Kommunfullmäktige) matches.
- On re-vet ("contamination sweep") runs, re-derive all metadata, decisions
  and sub-entities (proposals, votes, aggregate votes, reservations, protocol
  notes) from the document instead of trusting persisted values — persisted
  sub-entity counts can be empty even when the source records full
  omröstningslistor.
