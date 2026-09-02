# helsingborg.se decision extraction (kf = Kommunfullmäktige)

- KF protocols use a "Kommunfullmäktiges beslut" block per §; extract those
  blocks verbatim, keeping all "att"-points.
- §1-type "Inledning och val av justerare" (election of justerare + ersättare)
  is a decision; include it.
- Counted omröstningar on amendments (yrkanden) are explicit decision
  outcomes: extract each as a separate decision with outcome "Avslag"/"Bifall"
  and voting_method including the tally, e.g. "Omröstning (36 ja, 29 nej,
  0 avstår)". Chair-led propositions without a recorded vote are part of
  beslutsgång; skip them.
- "Anmälningar" whose beslut is only "att lägga anmälningarna till
  handlingarna" are announcements; skip. "Frågor i kommunfullmäktige" with no
  questions has no decision; skip.
- Bordläggning decisions are kept: "Avslut av sammanträdet" (avsluta +
  bordlägga interpellationer) and per-interpellation "att bordlägga
  interpellationen".
- Reservation notes are not part of the decision text; skip.
- Politicians: include beslutande ledamöter (role Ledamot / Ordförande),
  tjänstgörande ersättare (tag "tjänstgörande ersättare") and ej tjänstgörande
  ersättare (tag "ej tjänstgörande ersättare"). Staff (sekreterare,
  stadsdirektör etc.) are not politicians; skip.
