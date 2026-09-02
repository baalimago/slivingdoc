# Åtvidaberg KF decision extraction guidance

- KF protocols structure: each paragraph has a "Kommunfullmäktiges beslut"
  block; the decision text follows "Kommunfullmäktige beslutar ..." /
  "Kommunfullmäktige godkänner/antar/fastställer ...". Record each such block
  as a decision.
- Skip paragraphs whose beslut block only says "Kommunfullmäktige noterar
  informationen till protokollet." — pure information items (e.g. recording
  of final election result/mandate distribution, redovisning of
  medborgarförslag). Not decision outcomes.
- Election paragraphs (val av presidium, valberedning, valnämndsersättare)
  are real decisions; keep them, including the full lists of elected persons.
- Keep report/audit paragraphs only when the beslut block contains an action
  beyond filing, e.g. "överlämna granskningsrapporten till kommunstyrelsen
  för beredning".
- Voting: when a votering occurred, results appear both in the paragraph text
  ("Vid votering avges X JA-röster och Y NEJ-röster") and in a bilaga
  "Voteringsresultat" with per-member votes and totals. Put the totals in
  voting_method; omit voting_method when no vote is recorded (proposition
  only).
- Reservationer are listed under the beslut block; note them in the summary
  when present.
- Politicians can be taken from bilaga 1 (närvarorapport): ledamöter and
  tjänstgörande ersättare with party; note who replaced whom.
