# Heby KF decision extraction guidance

- Standard KF protocol (Ciceron/Public360 style): each § has a "Kommunfullmäktiges
  beslut" bullet block; extract those blocks as decisions.
- Skip information items whose beslut block only notes/thanks: "Kommunfullmäktige
  tackar för informationen" (bolagsinformation, styrelser/nämnder, frågor,
  anmälningsärenden). Skip empty items ("Det föreligger inga ...").
- Election, avsägelse (entledigande), bordläggning and återremiss paragraphs are
  decisions and are kept.
- Quirk: in some §§ the votering section can contradict the beslut block (e.g. both
  §33 and §35 record "återremitteras" as the decision while the votering shows 21 Ja
  for deciding today vs 18 Nej for remit). Extract the beslut block as the decision
  outcome and transcribe the votering tally verbatim in voting_method; do not
  reconcile.
- Politicians: beslutande ledamöter (role Ledamot, ordförande has role Ordförande),
  ej tjänstgörande ersättare (role Ersättare, tag "ej tjänstgörande ersättare",
  presence range e.g. "§ 23 - 31" as tag). Kommundirektör/kommunsekreterare/VD are
  staff; skip.
