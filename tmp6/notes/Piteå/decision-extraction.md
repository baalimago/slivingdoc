# Piteå KF decision extraction notes

## Protocol format (Sammanträdesprotokoll Kommunfullmäktige)
- Each § has a "Beslut" section followed by "Ärendebeskrivning", "Förslag",
  "Beslutsgång", sometimes "Reservation"/"Beslutsunderlag".
- Extract the "Beslut" text as the decision. "Beslutsgång" usually records no
  formal vote; when only one proposal exists, voting_method "Acklamation" is
  the standard interpretation. Some Piteå protocols DO contain formal votes:
  an "Omröstning begärs ..." section with "Omröstningsresultat" and a voter
  list (e.g. digital omröstning on motions) — extract the votes and
  party-aggregate votes from the list in that case instead of assuming
  acklamation.

## Skip / include conventions
- Skip sections with no "Beslut" section, e.g. ajournering/utdelning items.
- Skip pure information items whose Beslut only says "tar del av anmälda
  handlingar" (take-note of received documents).
- Include "godkänner redovisning ..." items (formal approval of reports).
- Include motion remittering ("remitterar motionen ... för beredning") as a
  decision with outcome "Remitterad"; motion avslag as "Avslag".
