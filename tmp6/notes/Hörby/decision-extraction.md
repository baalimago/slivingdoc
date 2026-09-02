# Hörby KF decision extraction guidance

- KF protocols (Ciceron PDF) use a "Kommunfullmäktiges beslut" block per § with
  "att"-bullets; extract those blocks as decisions. Every § 186..206 here carries one.
- Skip information items even though they have a beslut block: e.g. § "Kommunstyrelsens
  ordförande informerar" (beslut only "Informationen noteras till protokollet").
- Skip announcements/meddelanden items: e.g. "Meddelande kommunfullmäktige" whose beslut
  is only "Godkänna sammanställningen över meddelanden" (functionally lägga till
  handlingarna).
- Include procedural opening items (Upprop, godkännande av kungörelse/föredragningslista,
  val av justerare) — they record explicit decision outcomes.
- Include interpellation items with both decisions ("får ställas och besvaras" +
  "anses besvarade").
- Counted voteringar print results in the § ("18 Ja-röster och 13 Nej-röster"); put the
  tally + which yrkande each side supported in voting_method. Chair-led propositions
  without a recorded vote: omit voting_method.
- Reservation notes are not part of the decision text; skip.
- Politicians: beslutande ledamöter (role Ledamot/Ordförande), tjänstgörande ersättare
  (tag "tjänstgör för X (party)"), ej tjänstgörande ersättare listed separately (tag
  "ej tjänstgörande ersättare"). Sekreterare is staff; skip.
- Politicians who serve only part of the meeting carry range tags (e.g. "§§ 188-205",
  "närvarande §§ 186-194, 196-205"); a person can be both tjänstgörande (§195) and ej
  tjänstgörande (other §§) — keep both in identifiable_tags on one entry.
