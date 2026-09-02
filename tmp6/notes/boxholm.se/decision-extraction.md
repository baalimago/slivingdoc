# boxholm.se chain — decision extraction guidance

- Chain target is Boxholm, but this chain has received Kommunfullmäktige
  protocols from OTHER municipalities (reviewer contamination sweeps flagged a
  persisted organization that did not match the chain target). The document
  text is authoritative: derive organization_name / organization_level /
  organization_location from the protocol itself, never from the chain target,
  and do NOT reject solely because the municipality differs from Boxholm.
  Reject only when the meeting type does not match the expected type
  (Kommunfullmäktige).
- On re-vet runs, a persisted "current meeting" from another municipality must
  not be reused; re-derive the whole meeting (metadata, decisions,
  sub-entities) from the document text.
