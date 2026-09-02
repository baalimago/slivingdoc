# Simrishamn KF decision extraction guidance

- KF protocols use a "KOMMUNFULLMÄKTIGES BESLUT" block per §; keep all bullets of a §
  in one decision entry, paragraph_number as "§N" matching the ärendelista.
- Include procedural opening items (§ Val av justerare, § Fastställande av ärendelista).
- Skip "Meddelanden" § (here the last item, sometimes spanning several sub-items in the
  table of contents) — announcements only, no decision.
- No voteringar recorded in these protocols: omit voting_method unless a vote tally prints.
- Politicians: the "Närvarande" roster on page 2 mixes ledamöter and tjänstgörande
  ersättare ("X tjänstgör för Y (party)"). Tjänstgörande ersättare get role
  "Tjänstgörande ersättare" + tag "tjänstgör för <name>". Members serving only part of
  the meeting carry range tags copied from the roster ("§§ 57-64, 66"). Jäv handling:
  replaced members (e.g. jäv for §65) keep their range tag; the substituting ersättare
  gets the § tag for that paragraph. The "Övriga deltagande" list = ej tjänstgörande
  ersättare + officials (kommundirektör/kanslichef/nämndsekreterare); officials skip.
- Party list: M, S, SD, C, V, L, MP, KD, ÖP, KIS. Roster may contain typos (e.g. "((SD)")
  — normalize to the intended party.
