# bollebygd.se chain notes

- This chain's target municipality is Bollebygds kommun (Västra Götalands län).
- A systematic contamination sweep may flag extracted organizations as
  mismatched against "Bollnäs" (a different chain's target). The protocol text
  is authoritative: protocols naming "Bollebygds kommun" (Bollebygdskolans
  matsal, Bollebo, Sjuhärads samordningsförbund, Borås Stad members) belong to
  Bollebygd, not Bollnäs. Keep the municipality named in the document.
- The local party "FR" appears in Bollebygd KF protocols (e.g. Motion (FR)).
  The party name is spelled out as "Folkets Röst" in partistöd items (e.g.
  "Partistöd 2025 - Folkets Röst"); canonicalization.md maps fr → Folkets röst,
  so use the canonical party value "Folkets röst".
- Bollebygd KF protocols render roll-call votes as a separate "Voteringslista"
  page following the paragraph (one row per ledamot with Ja/Nej/Avstår columns
  and a Resultat row); check for it when a paragraph records an omröstning.
  In plain-text extraction the X marks often lose their column alignment and
  multi-page lists shift indentation, so per-person vote direction may be
  unreliable; prefer the narrative totals. A paragraph may hold two roll calls
  with different outcomes (e.g. §71: Ulf Rapp, then Ingridh Anderén), which a
  single per-decision votes array cannot cleanly separate.
