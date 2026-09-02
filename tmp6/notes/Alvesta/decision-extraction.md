# Alvesta KF decision extraction

- The scoped target dir notes/Alvesta/ may be empty; Alvesta-specific guidance
  lives in notes/alvesta.se/README.md and applies to this chain.
- Key rule: Kommunfullmäktige protocol items carry a "Beslut" heading, but items
  that merely "noterar informationen" (Meddelanden, Aktuellt från revisorerna,
  Redovisning av SAM, information about revisionsberättelse) are information
  items and must be skipped. Only Beslut sections with a substantive outcome
  (godkänna, anta, utse, entlediga, fastställa, ändra datum, uppdrag till nämnd,
  motion till KS för beredning) are decisions; procedural decisions (närvaro,
  val av justerare, godkännande av dagordning) are recorded as decisions.
- Local party "AA" appears in attendance/vote lists without a spelled-out name;
  submit verbatim (no canonical expansion known from source).
- Votes: counted "Votering" results appear both in the § text and in a voting
  bilaga; record per-politician votes from the bilaga and per-party
  aggregate_votes. "Beslutsgång" without a counted vote carries no votes array.

- UPDATE 2026-09-01: the shared canonicalization.md now maps "AA → Alvesta Alternativet";
  submit the canonical party value "Alvesta Alternativet" for local party AA instead of
  verbatim "AA" (the shared mapping takes precedence over the earlier verbatim guidance).
