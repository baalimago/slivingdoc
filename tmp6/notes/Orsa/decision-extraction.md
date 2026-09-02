# Orsa (Orsa) - KF decision extraction notes

Target: Orsa kommun, meeting type Kommunfullmäktige (KF).

## Protocol structure (KF minutes)
- Decisions appear in a 'Beslut' block under each numbered paragraph (§); extract those.
- §1 'Mottagande' lists medborgarförslag/motioner that are handed over
  ('överlämnas till kommunstyrelsen för beredning'); the referral is a decision
  outcome - record as one decision covering the whole paragraph.
- 'Informationsärenden' and 'Delgivningar' paragraphs are information only - skip.
- A 'Beslutsgång' with 'proposition' (chair asks and finds approval) is not a
  recorded vote; omit voting_method unless an actual vote is recorded.
- Attendance comes from a separate 'närvaro- och omröstningslista' attachment; if
  it is not in the text, list politicians identifiable from the protocol body
  (chair, justerare, named office holders and proposers with party where given).
