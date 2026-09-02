# Östra Göinge KF decision extraction guidance

- Decisions appear under a "Kommunfullmäktiges beslut" heading per §; keep all bullets of a
  § in one decision entry, paragraph_number as "§N" matching the ärendelista.
- Include procedural opening items (§ Val av justerare, § Godkännande av dagordning) — they
  record explicit decision outcomes.
- Skip "Anmälan av ..." items (§ with beslut "läggs till handlingarna") — they are
  announcements/noting decisions, not substantive decisions.
- Skip "Meddelanden till kommunfullmäktige" (beslut "Redovisningen godkänns") — announcement.
- Interpellation items do record explicit decisions (interpellationen får ställas / svaret
  noteras) and are included as decisions.
- When a § has a Beslutsgång paragraph stating "via acklamation", put "Acklamation" in
  voting_method.
- Politicians: "Beslutande" roster on page 1; tjänstgörande ersättare get role
  "Tjänstgörande ersättare" with identifiable tag "tjänstgör för <name>"; the substituted
  person is not listed separately. Ej tjänstgörande ersättare are not decision-makers; skip.
- Retrieval mechanics (Unikom/Ciceron) live in /tmp/sakfraga-notebook/notes/ostragoinge.se/kf-retrieval.md.
