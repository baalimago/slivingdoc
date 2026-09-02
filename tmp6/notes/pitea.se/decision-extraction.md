# Piteå KF decision extraction guidance

- Piteå KF protocols use a per-§ layout: "Beslut" block, then Ärendebeskrivning,
  Yrkanden, Propositionsordning, Beslutsunderlag. Extract the "Beslut" block.
- Skip pure information items whose beslut only says "Kommunfullmäktige tar del av
  informationen" (e.g. §236-237 style) and "Redovisning av anmälda handlingar"
  ("tar del av anmälda handlingar") - noting, not a decision.
- Keep: avsägelser + fyllnadsval (godkänna avsägelse / utse ny ledamot), partistöd
  items ("beslutar att utbetala kommunalt partistöd"), motionssvar (bifalla/avslå/
  delvis bifall), motioner remitterade för beredning (says which nämnd), and
  interpellationsbeslut ("anser interpellationen besvarad" or "får ställas till X").
- Reservations are printed after the Beslut block - include them in full_text.
- Votering lists may appear as separate pages after the § (e.g. §249 two digital
  voteringar, §276 återremiss-votering). Record voting_method with the counts when
  present; otherwise omit voting_method (acclamation).
- Party codes seen: S, C, M, SLP (Skol- och landsbygdspartiet), KD, MP,
  SJV (Sjukvårdspartiet), SD, V.
