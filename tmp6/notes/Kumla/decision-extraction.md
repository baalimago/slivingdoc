# Kumla KF decision extraction guidance

- Kumla kommunfullmäktige protocols follow the same conventions as
  Karlskoga/Köping KF: extract only "Kommunfullmäktiges beslut" blocks; skip
  paragraphs whose beslut is "lägger informationen till handlingarna"
  (information items, e.g. revisionsrapport, inkomna skrivelser, tertialrapport
  when merely laid to the minutes).
- Include procedural formal decisions: val av protokollsjusterare (§1-type,
  outcome "Bifall"), godkännande av årsredovisningar (outcome "Godkänd"),
  election/appointment items (outcome "Val genomfört"), motion responses
  (outcome "Bifall").
- Voting method "Proposition" only when the ordförande ställde förslag mot
  yrkande and found the council's decision; otherwise omit.
- Politicians: "Beslutande" roster (role Ledamot; Ordförande/Vice
  ordförande/2:e vice ordförande for office holders) plus "Ersättare"
  (role Ersättare). Skip "Övriga" and "Frånvarande".
- OCR strips å/ä/ö from names; restore standard spellings where unambiguous.
