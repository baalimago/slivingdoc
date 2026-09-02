# Munkfors KF decision extraction guidance

- Munkfors KF protocols (Sammanträdesprotokoll, Kommunfullmäktige) record
  decisions in a "Kommunfullmäktiges beslut" block per §. Extract those blocks.
- Skip paragraphs whose beslut is only an anmälan/noting, e.g. "anses anmält
  och läggs till handlingarna" (t.ex. Länsstyrelsens slutliga rösträkning/val-
  protokoll anmäls).
- Keep "godkänner rapporteringen" beslut blocks (redovisning av motioner,
  rapportering SoL/LSS) as approvals, not as pure information.
- Keep remittering of medborgarförslag ("överlämnas till kommunstyrelsen för
  beredning") as decisions.
- Election/appointment blocks ("Val av ...") are decisions; keep mandate period
  in full_text.
- No counted omröstningar recorded; omit voting_method.
