# Älmhult KF decision extraction

- Scoped target dir is notes/Älmhult/ (umlaut). Älmhult retrieval worklog lives in
  notes/almhult.se/kf-retrieval.md (SiteVision PDFs, filename
  "Kommunfullmäktiges sammanträdesprotokoll YYYY-MM-DD.pdf").
- Protocol format: "Sammanträdesprotokoll <date> / Kommunfullmäktige", paragraph
  range on front page, per-§ blocks headed "Kommunfullmäktiges beslut" with a
  bold decision text after "-" or numbered points. "Beslutsnivå: Kommunfullmäktige"
  follows each beslut block.
- Keep: val av justerare, fastställande av dagordning, bordläggningar
  ("Bordlägga ärendet"), noteringar av rapporter, motion svar (ansa/avslå),
  årsredovisning + ansvarsfrihet, enkla frågor ("För var och en av frågorna har
  kommunfullmäktige beslutat att frågan får ställas"), interpellationer
  ("Interpellationen får ställas" + "anses besvarad"), motioner överlämnade till
  nämnd för beredning.
- Skip: items headed only "Information" (e.g. kalendarium, nämndinformation,
  projektredovisning) — no "Kommunfullmäktiges beslut" block.
- Voting: decisions are made via "Beslutsgång" where ordförande asks and "finner
  att kommunfullmäktige beslutat så" — no counted omröstningar; omit voting_method
  and votes. Record "Förslag på sammanträdet" (yrkanden) as proposals and
  "Reservation" blocks as reservations when present.
- Närvarolista: "Beslutande / Ordinarie ledamöter" (with "(M), ordförande" suffix
  for the chair) plus "Tjänstgörande ersättare" listed as "X (P) ers: Y (P)" —
  put the ers: info in identifiable_tags.
- Metadata: organization_name "Älmhults kommun", organization_location "Älmhult",
  organization_level "Kommun", meeting_type "Kommunfullmäktige".
