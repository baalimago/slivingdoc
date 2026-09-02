# Ekerö KF decision extraction guidance

- Decisions appear under a "Beslut" heading per §; extract those blocks.
- Skip information items even when wrapped in "Beslut": "Anmälningar för
  kännedom" ("noterar att inga anmälningar inkommit") and nämnd "rapportering"
  ("noterar informationen").
- Ajournering paragraphs (meeting recess) are not formal decisions; skip.
- Question/interpellation sessions close with "Kommunfullmäktige anser
  frågorna/interpellationerna besvarade" - recorded decisions, include.
- Procedural items are included: upprop ("noterar uppropet"), val av justerare/
  rösträknare, bordläggning of an item.
- Interpellation/motion "för anmälan/remittering" paragraphs are decisions
  ("beslutar anmäla interpellationen", "remitterar motionen till kommunstyrelsen").
- Valärenden: one § with many numbered items (some "Bordlägges"); keep as ONE
  decision entry with full text of all items.
- Årsredovisning § produces two-part decisions (godkänna årsredovisning +
  bevilja ansvarsfrihet); keep both parts.
- §78/79-style "Ekerövatten AB:s styrelse föreslår kommunfullmäktige besluta att
  fastställa verksamhetsområde ..." is the adopted decision wording; the council
  fastställer per beslutsgång (bifall till liggande förslag).
- Voting: protocols use "Ordföranden konstaterar ..." without a recorded
  omröstning; omit voting_method unless an explicit vote result is printed.
- Motionssvar outcomes: "anse motionen besvarad" or "avslå motionen";
  reservations are noted but not part of the decision.
