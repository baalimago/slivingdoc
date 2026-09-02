# Bromölla KF decision extraction guidance

- KF protocols (PDF) use a "Kommunfullmäktiges beslut" block per §; keep all bullets of a
  § in one decision entry. paragraph_number as "§N" matching the ÄRENDELISTA, not the body
  header — body headers occasionally carry a typo copying the KS/utskotts § number
  (e.g. a body header "KF § 159" for the item listed as § 104 in the ärendelista).
- Skip "Meddelande till kommunfullmäktige" items (beslut "Redovisningen mottas") — announcements.
- Include motion responses, avsägelser ("Avsägelsen godkänns") and val items — they record
  explicit decision outcomes.
- Counted voteringar print results in the §; put the tally + what each side supported in
  voting_method. Chair-led propositions without a recorded vote: omit voting_method.
- Politicians: "Beslutande" roster (Ledamöter + Tjänstgörande ersättare) on the närvarolista.
  Bromölla's närvarolista does NOT state which ledamot each tjänstgörande ersättare replaces —
  no "tjänstgör för" tag available; keep only explicit range tags (e.g. "§§ 97-105").
  The voting lists include non-present members too; don't use them for attendance.
- Retrieval mechanics for bromolla.se live in /tmp/sakfraga-notebook/notes/bromolla.se/kf-retrieval.md.
