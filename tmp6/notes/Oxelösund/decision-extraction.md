# Oxelösund KF decision extraction

- KF protocols ("Sammanträdesprotokoll", SiteVision PDFs) number paragraphs
  "Kf §NN"; each § carries a "Kommunfullmäktiges beslut" block.
- Keep every § with an explicit beslut block, including "Informationen godkänns"
  items (§ Information och rapporter, § Information från KS ordförande) and
  motions "medges lämnas in och överlämnas till kommunstyrelsen för beredning".
- Skip § without a beslut block: "Allmänhetens frågestund" and "Frågor till
  kommunfullmäktige" when nothing was submitted.
- Voting: decisions taken by proposition ("ställer ... under proposition och
  finner att det bifalls"); no recorded omröstning, so omit voting_method even
  when a motförslag was defeated (§30, §41). Reservations are context, not part
  of the decision text.
- Politicians: "Beslutande" list is authoritative; "Ej tjänstgörande ersättare"
  are non-voting. Tjänstgörande ersättare serving only §31/§33 (jäv) also appear
  in the Beslutande list.
