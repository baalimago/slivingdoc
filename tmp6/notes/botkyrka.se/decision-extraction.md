# Botkyrka KF decision extraction guidance

- Decisions appear under a "Beslut" heading per §; extract those blocks.
- Skip information items even when wrapped in "Beslut": "Anmälningsärenden"
  ("Kommunfullmäktige noterar informationen till protokollet") and nämnd
  rapportering/yttranden.
- Interpellation answers close with "Interpellationen är besvarad" - recorded
  decisions, include. Same for "Enkel fråga" items ("1. Frågan får ställas.
  2. Frågan är besvarad.").
- "Nya motioner"/"Nya interpellationer" remittering/överlämnande paragraphs are
  decisions - include.
- Valärenden (Avsägelser och fyllnadsval): one § with many items (some
  bordlagda); keep as ONE decision entry with full text of all items.
- Reservations and "Särskilt yttrande" are noted but not part of the decision.
- "Deltar ej" lines (e.g. parties abstaining from a decision) belong inside the
  decision's full text.
- Voting: omit voting_method unless an explicit "Votering" with voteringsresultat
  is printed (then include the tally).
