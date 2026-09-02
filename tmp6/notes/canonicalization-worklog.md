# Canonicalization worklog

- 2026-06-15 (scan): created canonicalization.md. Added "O → --" because
  Region Gotland Regionfullmäktige voting bilagor render no-party members
  (Michael Stenberg, Johan Asplund) as "(O)" while the attendance list renders
  them as "(--)"; they are the same persons, so "O" is an alias for "--".
- 2026-06-17 (scan run): created canonicalization.md; added "BP → Bjärepartiet"
  after observing the Båstads kommun KF protocol use the abbreviation BP and
  spell out "Bjärepartiet" once (§127 väckt motion). Party values in the
  submission use the canonical form.
- 2026-06-24 scan (Melleruds kommun KF): created canonicalization.md with
  KiM → KIM and Kim → KIM. Mellerud KF protocols spell the local party "KIM" in
  attendance/vote lists but "KiM"/"Kim" in prose (e.g. "M+KD+KiM+SD",
  "SD, M, KD och KiM"); canonical form is the list spelling KIM.
- 2026-09-01: created canonicalization.md; added "BP → Bjärepartiet" after
  observing the Båstads kommun KF protocol use the abbreviation BP and spell
  out "Bjärepartiet" once (§127 väckt motion). Party values in the submission
  use the canonical form.
- conflict resolution: two concurrent edit branches had left merge-conflict
  markers in canonicalization.md and this worklog; merged both branches, keeping
  every mapping from both sides (s/c/v/sd/m/l/kd/mp party-name lines and
  BP → Bjärepartiet). No mappings added or removed.
- 2026-09-01: added "mp → Miljöpartiet" to canonicalization.md after observing
  (MP) used for Miljöpartiet in a Grästorps kommun KF protocol; party values in
  the submission use the canonical form.
- 2026-09-01 conflict resolution: resolved merge-conflict markers left by
  concurrent edits in canonicalization.md and this worklog; kept every mapping
  from both sides (incl. kd → Kristdemokraterna, mp → Miljöpartiet,
  BP → Bjärepartiet) and all prior worklog entries verbatim; appended the
  mp → Miljöpartiet entry.
- 2026-09-01 (boden.se chain re-vet of a Bergs kommun KF protocol): added
  "be → Bergspartiet" after observing the Bergs kommun KF protocol use the
  abbreviation "be" for Bergspartiet in the attendance list and election
  decisions (the party name is spelled out once in §144). Party values in the
  submission use Bergspartiet.
- 2026-09-01: canonicalization.md still contained unresolved merge-conflict
  markers ("<<<<<<< local / ======= / >>>>>>> remote") around the kd/mp/BP/be
  lines; resolved in place by deleting only the three marker lines, keeping
  every mapping from both sides (O → --, s/c/v/sd/m/l, kd → Kristdemokraterna,
  mp → Miljöpartiet, BP → Bjärepartiet, be → Bergspartiet). No mapping added or
  removed by this cleanup.
- 2026-09-01: observed (KD) used for Kristdemokraterna in a Bjuvs kommun KF
  protocol (e.g. Maria Berglund (KD)); the kd → Kristdemokraterna mapping is
  present in canonicalization.md (restored during conflict cleanup). Party
  values in the submission use the canonical form.
- 2026-09-01 (this run, Bergs kommun KF 2023-09-28): resolved the nested
  merge-conflict markers that reappeared in canonicalization.md and this
  worklog under concurrent edits; merged all branches into one lean file:
  O → --, s → Socialdemokraterna, c → Centerpartiet, v → Vänsterpartiet,
  sd → Sverigedemokraterna, m → Moderaterna, l → Liberalerna,
  kd → Kristdemokraterna, mp → Miljöpartiet, BP → Bjärepartiet and
  be → Bergspartiet. Every mapping from every branch is kept; no mapping
  removed. Note for later agents: the boden.se chain may receive protocols of
  other municipalities (e.g. Bergs kommun); the protocol text is authoritative
  for organization_name and the ljungby.se note documents the same rule.
- 2026-09-01 (bollebygd.se KF 2026-03-19): added "- → --" after observing
  Bollebygds kommun KF attendance list render no-party member Patrik Solerius
  as "(-)" (same meaning as "(O)"/"(--)" already mapped to "--").
  Party values in the submission use the canonical form.
- 2026-09-01 (this run, Bjuvs kommun KF re-vet): added "- → --" after
  observing Bjuvs kommun KF protocols render party-less members as "(-)"
  (e.g. Raymond Blixt (-), Sara Andersson (-)); the existing "O → --" line
  already establishes "--" as the canonical no-party marker, so "-" is a
  synonym. Party values in the submission use "--".
- 2026-09-01 (falun.se chain re-vet, contamination sweep): added
  "NE → Nystart Enköping" after observing the Enköpings kommun KF protocol use
  "NE" for the local party Nystart Enköping in attendance lists, vote lists and
  budget motions (the full name is spelled out in §141/§162). Party values in
  the submission use Nystart Enköping.
- 2026-09-01 (Alvesta kommun KF 2023-10-10 re-vet): added "AA → Alvesta Alternativet" to canonicalization.md after observing the Alvesta KF protocol use the abbreviation AA for Alvesta Alternativet in attendance lists and yrkanden (the party name is spelled out once in the §112 reservation). Party values in the submission use Alvesta Alternativet. Note: §115 lists Jan-Erik Svensson as "(A)" (typographical variant of AA); the attendance list is authoritative for the canonical party.
- 2026-09-01 (this run, Alvesta kommun KF 2023-03-01 re-vet): re-added
  "AA → Alvesta Alternativet" to canonicalization.md. The mapping was logged
  in the 2026-09-01 worklog entry but missing from the file (lost under a
  concurrent conflict-resolution rewrite). This Alvesta KF protocol confirms
  the mapping from its own text: the §18 reservation spells out "samtliga
  ledamöter i Alvesta Alternativet, Sverigedemokraterna och Moderaterna" while
  attendance lists and yrkanden use "AA". Party values in the submission use
  Alvesta Alternativet.

- 2026-09-01 (this run, Arvika kommun KF 2025-11-24): added "ArvP → Arvikapartiet"
  to canonicalization.md after observing the Arvika KF protocol use (ArvP)/(Arvp)
  for Arvikapartiet in the attendance list and yrkanden (the party is spelled out
  as "Arvikapartiet" in §209's partistöd decision). Party values in the submission
  use the canonical form Arvikapartiet.
- 2026-09-01 (this run, Arvika kommun KF 2023-05-29 re-vet): added
  "AP → Arvikapartiet" to canonicalization.md after observing the Arvika KF
  protocol render Arvikapartiet as "(AP)" in attendance lists and yrkanden
  (e.g. Lars-Olof Gävert (AP), Susanne Engstad (AP)) while reservations spell
  out "Arvikapartiets ledamöter"; ArvP (already mapped) and AP are the same
  local party. Party values in the submission use Arvikapartiet.
- 2026-09-01 (this run, Arvika kommun KF 2025-03-31 re-vet): added
  "ArvP → Arvikapartiet" to canonicalization.md after observing the Arvika KF
  protocol use "ArvP"/"Arvp" for Arvikapartiet in attendance lists, yrkanden
  and reservations (party name spelled out as "Arvikapartiet" in the §52 title
  and as "ArvikaPartiet" in §50). Party values in the submission use
  Arvikapartiet.
- 2026-09-01 (this run, berg.se chain re-vet of an Arjeplogs kommun KF
  protocol 2026-06-22): added "FoAr → Folkinitiativet Arjeplog" to
  canonicalization.md after observing the Arjeplogs kommun KF protocol use
  (FoAr) for the local party Folkinitiativet Arjeplog in attendance lists,
  yrkanden and reservations (the party name is spelled out as "Folkinitiativet
  Arjeplog" in the §56 motion text). Party values in the submission use
  Folkinitiativet Arjeplog.
- 2026-09-01 (this run, Nordanstigs kommun KF 2026-06-22 re-vet, contamination sweep): added
  "NOP → Nordanstigspartiet.se" and "nordanstigspartiet.se → Nordanstigspartiet.se" to
  canonicalization.md after observing the Nordanstig KF protocol render the local party as
  "(NOP)"/"(NoP)" in attendance and vote lists while §43 partistöd spells out the full name
  "nordanstigspartiet.se" (48 tkr). The reviewer's contamination flag about "Norrtälje" was a
  false positive: the protocol header, location (Bergsjö) and local party confirm Nordanstig.
  Party values in the submission use Nordanstigspartiet.se. (Entry re-appended: a concurrent
  commit rewrote this log and dropped the original append.)

- 2026-09-01 (this run, perstorp.se chain re-vet, contamination sweep of an
  Oskarshamns kommun KF protocol): added "klp → Kustlandspartiet" to
  canonicalization.md after observing the Oskarshamn KF protocol use "(KLP)"
  for the local party in the attendance list and yrkanden (the full name
  "Kustlandspartiet" is spelled out in §53's proposal list and reservation).
  Party values in the submission use Kustlandspartiet.
