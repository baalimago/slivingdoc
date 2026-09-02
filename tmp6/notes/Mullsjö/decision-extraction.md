# Mullsjö KF decision extraction

- Protocol layout (SAMMANTRÄDESPROTOKOLL, Kommunfullmäktige): per-§ blocks with
  Dnr, Ärendebeskrivning, Beslutsunderlag, "Förslag till beslut", then "Beslut"
  ("Kommunfullmäktige beslutar att ..."), sometimes "Beslutet skickas till".
- SKIP paragraphs whose beslut only files/notes information: "Information från
  ..." and "Delgivningar" (beslut = "att lägga ... till handlingarna").
- KEEP: godkännande av ärendelista, medborgarförslag (beslut "att anse
  medborgarförslaget besvarat"), godkännande av avtal, och avsägelser (one
  decision entry holding all att-clauses).
- Counted votes appear as an "Omröstning" block + "Omröstningslista" with JA/NEJ
  tallies; record voting_method when such an omröstning is documented.
