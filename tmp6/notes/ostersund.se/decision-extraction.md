# Östersund KF decision extraction guidance

- Extract only items under a "Kommunfullmäktiges beslut" heading with a substantive
  outcome (godkänna, anta, fastställa, bevilja, utse, avslå, bordlägga, återremittera,
  delegera val).
- Skip items whose beslut merely "tar del av informationen" (e.g. § Fördelning av fasta
  arvoden) — information items, even when wrapped in a Beslut heading.
- Skip "Meddelanden och informationer" and "Allmänhetens frågestund" blocks (no beslut).
- § Interpellationer och frågor records only that questions are allowed ("medger att
  interpellationen/frågan får ställas") — part of the question session, not a formal
  decision; skip.
- Procedural decisions are recorded: § Protokollets justering/upprop/dagordning
  (utseende av justerare) and bordläggning of an item are included as decisions.
- Annual-report paragraphs produce two-part decisions (godkänna årsredovisning +
  bemyndiga ombud för årsstämman); keep the full text of both parts.
- Valärenden (election matters) is one § with many numbered decisions; keep as one
  decision entry with the full text of all items.
- Voting: when an omröstning is held, record the vote result in voting_method
  (e.g. "39 ja mot 15 nej") and the winning outcome in outcome.
