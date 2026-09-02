# Nässjö KF decision extraction guidance

- Nässjö KF protocols have one "Kommunfullmäktiges beslut" block per §; that block is the
  decision to extract. Text after it (Sammanfattning, Kommunstyrelsens förslag, Yrkanden,
  Beslutsgång) is context, not the decision.
- Skip: § "Inkomna handlingar" (beslut = "läggs till handlingarna", pure noting),
  interpellations (§ 134-136 style: "Interpellationen får ställas ... är besvarad" =
  debate/Q&A items). Keep "Redovisning av ... godkänns" paragraphs (explicit approval).
- Keep: avsägelser (entledigas + ny utses), motion responses (also when motion is
  rejected), taxor/avgifter, budget, ägardirektiv.
- Budget § (verksamhetsplan/budget) carries many beslutsgångar with omröstningar 1..N and
  vote counts inline ("Med X röster ..."). Record the adopted main decision with the vote
  summary in voting_method; note reservations (per party) in summary.
- Politicians: the Beslutande list continues on the next page with § attendance ranges
  (e.g. "Rikard Salander (MP) §§ 132-144"); those are also decision-making members.
  "Övriga deltagare" includes both ersättare and officials - skip the officials
  (kommundirektör, enhetschef, sekreterare).
