# Eslöv KF decision extraction guidance

- KF protocols (Sammanträdesprotokoll, Kommunfullmäktige) list formal decisions
  under a "Beslut" heading per §. Extract those blocks; keep all bullet points.
- §1 "Val av justerare" is a decision - include (appointed justerare + ersättare).
- Information items are skipped even when they have a Beslut block:
  §2-type "Information från nämnd" (no decision) and "Anmälningar för
  kännedom" whose beslut is only "Redovisningen läggs till handlingarna".
- Reservations ("Ledamöterna i X reserverar sig...") and "Beslutsgång"
  (chair proposition) are not part of the decision text; skip them.
- No counted omröstning in these protocols; chair-led proposition without
  recorded vote -> omit voting_method.
- Avsägelse + fyllnadsval paragraphs are decisions (entlediga + utse ersättare);
  bordläggning of a fyllnadsval is part of the decision, keep.
