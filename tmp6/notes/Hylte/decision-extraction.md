# Hylte KF decision extraction guidance

- Hylte KF protocols (MÖTESPROTOKOLL, Kommunfullmäktige) record formal
  decisions in a "Beslut" block per §. Extract those blocks.
- Keep procedural decisions: "Val av justerare" and "Godkännande av
  ärendelistan" are decisions; keep them.
- Skip information items even when they carry a "Beslut" heading whose text is
  only "har tagit del av ..." / "har tagit emot informationen" (meddelanden,
  information från revisorerna, återrapportering).
- Some paragraphs have no "Beslut" heading but still record an explicit
  outcome (e.g. an interpellation paragraph where the ordförande decides the
  interpellation may not be posed); extract it as a decision.
- Keep motion remittering (tar emot + skickar till kommunstyrelsen för
  beredning) and avsägelser (tar emot + skickar till Länsstyrelsen för ny
  sammanräkning) as decisions.
- "Inga fyllnadsval behöver göras" / "Inga övriga ärenden" paragraphs have no
  decision; skip.
- When a counted votering is recorded (e.g. budget votes), put the vote
  description in voting_method; otherwise omit voting_method.
- Reservations ("reserverar sig mot beslutet") and "Yrkanden"/"Beslutsgång"
  are not part of the decision text; skip them.
