# Köping decision-extraction notes

## Kommunfullmäktige protocols — KF conventions
- Each decision paragraph has a `Beslut` heading with bullet points; extract the
  bullets verbatim as the decision full_text. Outcome is `Bifall` when the chair
  found the council decided in accordance with the proposal.
- Some standing announcement paragraphs (`Anmälan av motioner/medborgarförslag`,
  `Anmälan av frågor och interpellationer`, `Anmälan av handlingar`,
  interpellations, `Vissa val m.m.`) also carry a `Beslut` block — extract them;
  do not skip just because the paragraph is an announcement type.
- Paragraphs without a `Beslut` block (e.g. `Allmänhetens frågestund`,
  `Meddelanden`) are information items — skip.
- Voting: usually "Ordförande finner ..." (acclamation). When the chair sets two
  yrkanden against each other (§31 budget), record that as the voting_method.
- Members are listed under Beslutande/Ersättare on page 2; capture with party
  and role (Ordförande, 1:e/2:e vice ordförande, tjänstgörande ersättare,
  Ersättare).
